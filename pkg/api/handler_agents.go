package api

import (
	"net/http"
)

func (s *APIServer) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	type agentInfo struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Model       string   `json:"model"`
		Default     bool     `json:"default"`
		DelegatesTo []string `json:"delegates_to,omitempty"`
		Loaded      bool     `json:"loaded"`
	}

	agents := make([]agentInfo, 0, len(s.cfg.Agents.List))
	for _, entry := range s.cfg.Agents.List {
		model := entry.Model
		if model == "" {
			model = s.cfg.Agents.Defaults.Model
		}
		loaded := false
		if env, err := s.pool.GetOrCreate(entry.ID); err == nil && env != nil {
			loaded = true
			if env.ModelName != "" {
				model = env.ModelName
			}
		}
		agents = append(agents, agentInfo{
			ID:          entry.ID,
			Name:        entry.Name,
			Model:       model,
			Default:     entry.Default,
			DelegatesTo: entry.DelegatesTo,
			Loaded:      loaded,
		})
	}

	if len(agents) == 0 {
		defaultID := s.cfg.ResolveDefaultAgentID()
		agents = append(agents, agentInfo{
			ID:      defaultID,
			Name:    "Default Agent",
			Model:   s.cfg.Agents.Defaults.Model,
			Default: true,
			Loaded:  true,
		})
	}

	writeOK(w, agents)
}

func (s *APIServer) handleAgentByID(w http.ResponseWriter, r *http.Request) {
	agentID := pathParam(r, "/api/v1/agents/")

	suffix := pathSuffix(r, "/api/v1/agents/"+agentID)
	if suffix == "/model" || suffix == "/model/" {
		s.handleSwitchModel(w, r, agentID)
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	entry := s.cfg.FindAgentEntry(agentID)
	defaults := s.cfg.ResolveAgentDefaults(agentID)

	info := map[string]any{
		"id":                   agentID,
		"model":                defaults.Model,
		"max_tokens":           defaults.MaxTokens,
		"temperature":          defaults.Temperature,
		"max_tool_iterations":  defaults.MaxToolIterations,
		"memory_window":        defaults.MemoryWindow,
		"context_window_tokens": defaults.ContextWindowTokens,
		"workspace":            defaults.Workspace,
	}

	if entry != nil {
		info["name"] = entry.Name
		info["default"] = entry.Default
		info["delegates_to"] = entry.DelegatesTo
		if entry.Tools != nil {
			info["tools_include"] = entry.Tools.Include
			info["tools_exclude"] = entry.Tools.Exclude
		}
	}

	writeOK(w, info)
}

func (s *APIServer) handleSwitchModel(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := readJSON(r, &req); err != nil || req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	if err := s.pool.SwitchModel(agentID, req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, "switch model failed: "+err.Error())
		return
	}

	writeOK(w, map[string]string{"status": "ok", "agent_id": agentID, "model": req.Model})
}
