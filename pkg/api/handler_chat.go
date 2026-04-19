package api

import (
	"context"
	"net/http"
	"time"

	"github.com/yangkun19921001/PP-Claw/bus"
)

func (s *APIServer) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Content   string   `json:"content"`
		AgentID   string   `json:"agent_id,omitempty"`
		SessionID string   `json:"session_id,omitempty"`
		Media     []string `json:"media,omitempty"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	if req.SessionID == "" {
		req.SessionID = "ui:direct"
	}

	start := time.Now()

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sub, unsub := s.bus.SubscribeOutbound()
	defer unsub()

	inbound := bus.NewInboundMessage("ui", "api-user", req.SessionID, req.Content)
	if s.wsHub != nil {
		inbound.Media = s.wsHub.resolveMediaPaths(req.Media)
	} else {
		inbound.Media = req.Media
	}
	s.bus.PublishInbound(inbound)

	for {
		select {
		case msg := <-sub:
			if msg.Channel != "ui" || msg.ChatID != req.SessionID {
				continue
			}
			if isProgress, ok := msg.Metadata["_progress"].(bool); ok && isProgress {
				continue
			}
			writeOK(w, map[string]any{
				"content":     msg.Content,
				"session_id":  req.SessionID,
				"media":       msg.Media,
				"duration_ms": time.Since(start).Milliseconds(),
			})
			return
		case <-ctx.Done():
			writeError(w, http.StatusGatewayTimeout, "timeout waiting for response")
			return
		}
	}
}

func (s *APIServer) handleChatWS(w http.ResponseWriter, r *http.Request) {
	s.wsHub.handleWS(w, r)
}
