package api

import (
	"net/http"
)

func (s *APIServer) handleChannelsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.channelMgr == nil {
		writeOK(w, map[string]any{
			"mode":    "agent",
			"message": "channel manager not available in agent mode",
		})
		return
	}

	snapshot := s.channelMgr.StatusSnapshot()
	writeOK(w, snapshot)
}
