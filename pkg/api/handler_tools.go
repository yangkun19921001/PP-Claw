package api

import (
	"net/http"
)

func (s *APIServer) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	defaultID := s.cfg.ResolveDefaultAgentID()
	env, err := s.pool.GetOrCreate(defaultID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent env: "+err.Error())
		return
	}

	defs := env.Tools.GetDefinitions()
	if defs == nil {
		defs = []map[string]any{}
	}
	writeOK(w, defs)
}

func (s *APIServer) handleToolByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	name := pathParam(r, "/api/v1/tools/")
	defaultID := s.cfg.ResolveDefaultAgentID()
	env, err := s.pool.GetOrCreate(defaultID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent env")
		return
	}

	tool := env.Tools.Get(name)
	if tool == nil {
		writeError(w, http.StatusNotFound, "tool not found: "+name)
		return
	}

	writeOK(w, map[string]any{
		"name":        tool.Name(),
		"description": tool.Description(),
		"parameters":  tool.Parameters(),
	})
}
