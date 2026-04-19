package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/yangkun19921001/PP-Claw/agent"
	"github.com/yangkun19921001/PP-Claw/bus"
	"github.com/yangkun19921001/PP-Claw/channels"
	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/cron"
	"go.uber.org/zap"
)

type APIServer struct {
	cfg        *config.Config
	cfgPath    string
	bus        *bus.MessageBus
	agentLoop  *agent.AgentLoop
	pool       *agent.AgentEnvPool
	cronService *cron.Service
	channelMgr *channels.Manager
	logger     *zap.Logger
	wsHub      *WSHub
	srv        *http.Server
	mode       string
}

type APIServerConfig struct {
	Config     *config.Config
	ConfigPath string
	Bus        *bus.MessageBus
	AgentLoop  *agent.AgentLoop
	Pool       *agent.AgentEnvPool
	CronService *cron.Service
	ChannelMgr *channels.Manager
	Logger     *zap.Logger
	Mode       string
}

func New(c *APIServerConfig) *APIServer {
	logger := c.Logger
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	s := &APIServer{
		cfg:        c.Config,
		cfgPath:    c.ConfigPath,
		bus:        c.Bus,
		agentLoop:  c.AgentLoop,
		pool:       c.Pool,
		cronService: c.CronService,
		channelMgr: c.ChannelMgr,
		logger:     logger,
		mode:       c.Mode,
	}
	if s.bus != nil {
		ws := config.ExpandHome(s.cfg.Agents.Defaults.Workspace)
		s.wsHub = newWSHub(s.bus, logger, filepath.Join(ws, "uploads"))
	}
	return s
}

func (s *APIServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/system/status", s.handleSystemStatus)
	mux.HandleFunc("/api/v1/system/version", s.handleSystemVersion)

	mux.HandleFunc("/api/v1/config", s.handleConfig)
	mux.HandleFunc("/api/v1/config/", s.handleConfigSection)

	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/agents/", s.handleAgentByID)

	mux.HandleFunc("/api/v1/chat/send", s.handleChatSend)
	if s.wsHub != nil {
		mux.HandleFunc("/api/v1/chat/ws", s.handleChatWS)
	}

	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/sessions/", s.handleSessionByKey)

	mux.HandleFunc("/api/v1/channels/status", s.handleChannelsStatus)

	mux.HandleFunc("/api/v1/cron/jobs", s.handleCronJobs)
	mux.HandleFunc("/api/v1/cron/jobs/", s.handleCronJobByID)

	mux.HandleFunc("/api/v1/tools", s.handleTools)
	mux.HandleFunc("/api/v1/tools/", s.handleToolByName)

	mux.HandleFunc("/api/v1/skills", s.handleSkills)
	mux.HandleFunc("/api/v1/skills/", s.handleSkillByName)

	mux.HandleFunc("/api/v1/files/upload", s.handleFileUpload)
	mux.HandleFunc("/api/v1/files/", s.handleFileServe)
}

func (s *APIServer) Handler() http.Handler {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	var handler http.Handler = mux
	handler = corsMiddleware(handler)
	handler = recoveryMiddleware(s.logger, handler)
	handler = loggingMiddleware(s.logger, handler)
	return handler
}

func (s *APIServer) StartStandalone(ctx context.Context, addr string) error {
	listener, err := s.findAvailableListener(addr)
	if err != nil {
		return fmt.Errorf("api server listen failed: %w", err)
	}

	s.srv = &http.Server{Handler: s.Handler()}
	s.logger.Info("API server starting", zap.String("addr", listener.Addr().String()), zap.String("mode", s.mode))

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.srv.Shutdown(shutdownCtx)
	}()

	if err := s.srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *APIServer) Shutdown(ctx context.Context) error {
	if s.srv != nil {
		return s.srv.Shutdown(ctx)
	}
	return nil
}

func WrapMiddleware(logger *zap.Logger, handler http.Handler) http.Handler {
	return corsMiddleware(recoveryMiddleware(logger, loggingMiddleware(logger, handler)))
}

func (s *APIServer) findAvailableListener(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}

	host, _, _ := net.SplitHostPort(addr)
	for port := 18791; port <= 18800; port++ {
		fallback := fmt.Sprintf("%s:%d", host, port)
		ln, err = net.Listen("tcp", fallback)
		if err == nil {
			s.logger.Warn("primary port busy, using fallback", zap.String("addr", fallback))
			return ln, nil
		}
	}
	return nil, fmt.Errorf("no available port in range 18790-18800")
}
