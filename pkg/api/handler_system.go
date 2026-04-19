package api

import (
	"net/http"
	"time"

	"github.com/yangkun19921001/PP-Claw/agent"
)

var startTime = time.Now()

func (s *APIServer) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status := map[string]any{
		"version":    agent.GetVersion(),
		"commit":     agent.GetCommit(),
		"build_time": agent.GetBuildTime(),
		"mode":       s.mode,
		"uptime_s":   int(time.Since(startTime).Seconds()),
		"model":      s.cfg.Agents.Defaults.Model,
		"api_port":   s.cfg.Gateway.Port,
	}

	if s.channelMgr != nil {
		status["channels"] = s.channelMgr.EnabledChannels()
	}
	if s.cronService != nil {
		status["cron_jobs"] = len(s.cronService.ListJobs(false))
	}

	writeOK(w, status)
}

func (s *APIServer) handleSystemVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeOK(w, map[string]string{
		"version":    agent.GetVersion(),
		"commit":     agent.GetCommit(),
		"build_time": agent.GetBuildTime(),
	})
}
