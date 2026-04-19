package api

import (
	"net/http"

	"github.com/yangkun19921001/PP-Claw/cron"
)

func (s *APIServer) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	if s.cronService == nil {
		writeOK(w, map[string]any{
			"jobs":    []any{},
			"message": "cron service not available in agent mode",
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		jobs := s.cronService.ListJobs(true)
		writeOK(w, jobs)
	case http.MethodPost:
		s.handleCreateCronJob(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *APIServer) handleCreateCronJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string            `json:"name"`
		Message        string            `json:"message"`
		Schedule       cron.CronSchedule `json:"schedule"`
		Deliver        bool              `json:"deliver"`
		Channel        string            `json:"channel"`
		Account        string            `json:"account"`
		To             string            `json:"to"`
		DeleteAfterRun bool              `json:"delete_after_run"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if req.Name == "" || req.Message == "" {
		writeError(w, http.StatusBadRequest, "name and message are required")
		return
	}

	job := s.cronService.AddJob(req.Name, req.Schedule, req.Message, req.Deliver, req.Channel, req.Account, req.To, req.DeleteAfterRun)
	writeJSON(w, http.StatusCreated, job)
}

func (s *APIServer) handleCronJobByID(w http.ResponseWriter, r *http.Request) {
	if s.cronService == nil {
		writeError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}

	jobID := pathParam(r, "/api/v1/cron/jobs/")
	suffix := pathSuffix(r, "/api/v1/cron/jobs/"+jobID)

	if suffix == "/enable" || suffix == "/enable/" {
		s.handleCronJobEnable(w, r, jobID)
		return
	}
	if suffix == "/run" || suffix == "/run/" {
		s.handleCronJobRun(w, r, jobID)
		return
	}

	switch r.Method {
	case http.MethodGet:
		jobs := s.cronService.ListJobs(true)
		for _, j := range jobs {
			if j.ID == jobID {
				writeOK(w, j)
				return
			}
		}
		writeError(w, http.StatusNotFound, "job not found")
	case http.MethodDelete:
		if s.cronService.RemoveJob(jobID) {
			writeOK(w, map[string]string{"status": "removed", "id": jobID})
		} else {
			writeError(w, http.StatusNotFound, "job not found")
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *APIServer) handleCronJobEnable(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	job := s.cronService.EnableJob(jobID, req.Enabled)
	if job == nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeOK(w, job)
}

func (s *APIServer) handleCronJobRun(w http.ResponseWriter, r *http.Request, jobID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	jobs := s.cronService.ListJobs(true)
	for _, j := range jobs {
		if j.ID == jobID {
			writeOK(w, map[string]string{"status": "triggered", "id": jobID, "message": j.Payload.Message})
			return
		}
	}
	writeError(w, http.StatusNotFound, "job not found")
}
