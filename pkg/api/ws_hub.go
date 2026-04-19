package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yangkun19921001/PP-Claw/bus"
	"go.uber.org/zap"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type WSHub struct {
	bus        *bus.MessageBus
	clients    map[*WSClient]bool
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
	logger     *zap.Logger
	uploadDir  string
}

type WSClient struct {
	hub       *WSHub
	conn      *websocket.Conn
	send      chan []byte
	sessionID string
	done      chan struct{}
}

type WSEvent struct {
	Type       string         `json:"type"`
	Channel    string         `json:"channel,omitempty"`
	ChatID     string         `json:"chat_id,omitempty"`
	Content    string         `json:"content,omitempty"`
	Media      []string       `json:"media,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Timestamp  int64          `json:"timestamp"`
	SessionKey string         `json:"session_key,omitempty"`
}

type WSIncoming struct {
	Type      string   `json:"type"`
	Content   string   `json:"content"`
	SessionID string   `json:"session_id"`
	AgentID   string   `json:"agent_id,omitempty"`
	Media     []string `json:"media,omitempty"`
}

func newWSHub(msgBus *bus.MessageBus, logger *zap.Logger, uploadDir string) *WSHub {
	h := &WSHub{
		bus:        msgBus,
		clients:    make(map[*WSClient]bool),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		logger:     logger,
		uploadDir:  uploadDir,
	}
	go h.run()
	go h.bridgeOutbound()
	return h
}

func (h *WSHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		}
	}
}

func (h *WSHub) bridgeOutbound() {
	sub, unsub := h.bus.SubscribeOutbound()
	defer unsub()

	for msg := range sub {
		if msg.Channel != "ui" {
			continue
		}
		event := WSEvent{
			Type:      "message",
			Channel:   msg.Channel,
			ChatID:    msg.ChatID,
			Content:   msg.Content,
			Media:     msg.Media,
			Metadata:  msg.Metadata,
			Timestamp: time.Now().UnixMilli(),
		}
		if isProgress, ok := msg.Metadata["_progress"].(bool); ok && isProgress {
			event.Type = "progress"
		}
		if _, ok := msg.Metadata["_tool_hint"].(bool); ok {
			event.Type = "tool_hint"
		}
		if status, ok := msg.Metadata["_tool_status"].(string); ok {
			event.Type = "tool_" + status
		}
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		h.broadcast(data, msg.ChatID)
	}
}

func (h *WSHub) resolveMediaPaths(media []string) []string {
	if len(media) == 0 || h.uploadDir == "" {
		return media
	}
	resolved := make([]string, 0, len(media))
	for _, p := range media {
		if _, err := os.Stat(p); err == nil {
			resolved = append(resolved, p)
			continue
		}
		full := filepath.Join(h.uploadDir, filepath.Base(p))
		if _, err := os.Stat(full); err == nil {
			resolved = append(resolved, full)
		} else {
			resolved = append(resolved, p)
		}
	}
	return resolved
}

func (h *WSHub) broadcast(data []byte, chatID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if chatID != "" && client.sessionID != "" && client.sessionID != chatID {
			continue
		}
		select {
		case client.send <- data:
		default:
		}
	}
}

func (h *WSHub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", zap.Error(err))
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = "ui:direct"
	}

	client := &WSClient{
		hub:       h,
		conn:      conn,
		send:      make(chan []byte, 256),
		sessionID: sessionID,
		done:      make(chan struct{}),
	}
	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *WSClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
		close(c.done)
	}()

	c.conn.SetReadLimit(1024 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var incoming WSIncoming
		if err := json.Unmarshal(message, &incoming); err != nil {
			continue
		}
		if incoming.Type == "message" && incoming.Content != "" {
			sid := incoming.SessionID
			if sid == "" {
				sid = c.sessionID
			}
			c.sessionID = sid
			inbound := bus.NewInboundMessage("ui", "api-user", sid, incoming.Content)
			inbound.Media = c.hub.resolveMediaPaths(incoming.Media)
			c.hub.bus.PublishInbound(inbound)
		}
		if incoming.Type == "ping" {
			resp, _ := json.Marshal(WSEvent{Type: "pong", Timestamp: time.Now().UnixMilli()})
			select {
			case c.send <- resp:
			default:
			}
		}
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
