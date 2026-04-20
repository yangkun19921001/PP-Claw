package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/yangkun19921001/PP-Claw/config"
	"github.com/yangkun19921001/PP-Claw/session"
)

func (s *APIServer) getSessionManager() *session.Manager {
	defaultID := s.cfg.ResolveDefaultAgentID()
	env, err := s.pool.GetOrCreate(defaultID)
	if err != nil || env == nil {
		ws := config.ExpandHome(s.cfg.Agents.Defaults.Workspace)
		return session.NewManager(ws)
	}
	return env.Sessions
}

func (s *APIServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	mgr := s.getSessionManager()
	sessions := mgr.ListSessions()
	if sessions == nil {
		sessions = []map[string]any{}
	}
	writeOK(w, sessions)
}

func (s *APIServer) handleSessionByKey(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	raw = strings.TrimSuffix(raw, "/")
	key, _ := url.PathUnescape(raw)
	if key == "" {
		writeError(w, http.StatusBadRequest, "session key is required")
		return
	}

	mgr := s.getSessionManager()

	switch r.Method {
	case http.MethodGet:
		sess := mgr.GetOrCreate(key)
		writeOK(w, map[string]any{
			"key":        sess.Key,
			"messages":   sess.Messages,
			"created_at": sess.CreatedAt,
			"updated_at": sess.UpdatedAt,
		})
	case http.MethodDelete:
		sess := mgr.GetOrCreate(key)
		sess.Clear()
		mgr.Save(sess)
		writeOK(w, map[string]string{"status": "cleared", "key": key})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
