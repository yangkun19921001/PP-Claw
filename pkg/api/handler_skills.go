package api

import (
	"net/http"
	"strings"

	"github.com/yangkun19921001/PP-Claw/agent"
	"github.com/yangkun19921001/PP-Claw/learning"
)

func (s *APIServer) getSkillRepo(agentID string) (learning.SkillRepository, error) {
	path := s.cfg.ResolveSkillsPath(agentID)
	return learning.NewFileBasedSkillRepository(path, s.logger)
}

type skillItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"` // "workspace", "builtin", "embedded", "learned"
	Version     string   `json:"version,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Available   bool     `json:"available"`
	Path        string   `json:"path,omitempty"`
	UsageCount  int      `json:"usage_count,omitempty"`
	SuccessRate float64  `json:"success_rate,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
}

func (s *APIServer) handleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agentID := r.URL.Query().Get("agent_id")
		skillType := r.URL.Query().Get("type") // "all", "builtin", "learned"

		if agentID == "" {
			s.handleListAllSkills(w, r)
			return
		}

		if skillType == "builtin" {
			s.handleListBuiltinSkills(w, r, agentID)
			return
		}

		if skillType == "learned" {
			s.handleListLearnedSkills(w, r, agentID)
			return
		}

		// Default: return all skills (builtin + learned) for the agent
		s.handleListAgentAllSkills(w, r, agentID)

	case http.MethodPost:
		agentID := r.URL.Query().Get("agent_id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent_id query param required")
			return
		}

		if !s.cfg.IsLearningEnabled() {
			writeError(w, http.StatusServiceUnavailable, "learning not enabled")
			return
		}

		repo, err := s.getSkillRepo(agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var skill learning.Skill
		if err := readJSON(r, &skill); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		if skill.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if err := repo.SaveSkill(r.Context(), &skill); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		saved, _ := repo.GetSkill(r.Context(), skill.Name)
		if saved != nil {
			writeJSON(w, http.StatusCreated, saved)
		} else {
			writeJSON(w, http.StatusCreated, skill)
		}

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *APIServer) getBuiltinSkillsForAgent(agentID string) []skillItem {
	workspace := s.cfg.ResolveAgentWorkspace(agentID)
	loader := agent.NewSkillsLoader(workspace)
	skills := loader.ListSkills()

	var items []skillItem
	for _, sk := range skills {
		desc := loader.LoadSkill(sk.Name)
		descLine := extractDescription(desc)
		items = append(items, skillItem{
			Name:        sk.Name,
			Description: descLine,
			Source:      sk.Source,
			Available:   true,
			Path:        sk.Path,
		})
	}
	return items
}

func (s *APIServer) getLearnedSkillsForAgent(agentID string) []skillItem {
	if !s.cfg.IsLearningEnabled() {
		return nil
	}
	repo, err := s.getSkillRepo(agentID)
	if err != nil {
		return nil
	}
	metas, err := repo.ListSkills(nil)
	if err != nil {
		return nil
	}
	var items []skillItem
	for _, m := range metas {
		items = append(items, skillItem{
			Name:        m.Name,
			Description: m.Description,
			Source:      "learned",
			Version:     m.Version,
			Tags:        m.Tags,
			Available:   true,
			UsageCount:  m.UsageCount,
			SuccessRate: m.SuccessRate,
			CreatedAt:   m.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return items
}

func (s *APIServer) handleListAgentAllSkills(w http.ResponseWriter, r *http.Request, agentID string) {
	builtin := s.getBuiltinSkillsForAgent(agentID)
	learned := s.getLearnedSkillsForAgent(agentID)

	all := append(builtin, learned...)
	writeOK(w, all)
}

func (s *APIServer) handleListBuiltinSkills(w http.ResponseWriter, r *http.Request, agentID string) {
	items := s.getBuiltinSkillsForAgent(agentID)
	writeOK(w, items)
}

func (s *APIServer) handleListLearnedSkills(w http.ResponseWriter, r *http.Request, agentID string) {
	if !s.cfg.IsLearningEnabled() {
		writeOK(w, []any{})
		return
	}

	repo, err := s.getSkillRepo(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	query := r.URL.Query().Get("q")
	if query != "" {
		results, err := repo.FindSimilar(r.Context(), query, 20)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, results)
		return
	}

	skills, err := repo.ListSkills(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, skills)
}

type agentSkillsResponse struct {
	AgentID  string      `json:"agent_id"`
	Builtin  []skillItem `json:"builtin"`
	Learned  []skillItem `json:"learned"`
	AllCount int         `json:"all_count"`
}

func (s *APIServer) handleListAllSkills(w http.ResponseWriter, _ *http.Request) {
	// Only use agent IDs from config — directory scan picks up non-agent dirs
	agentIDs := make(map[string]bool)
	for _, a := range s.cfg.Agents.List {
		agentIDs[a.ID] = true
	}

	var result []agentSkillsResponse
	for id := range agentIDs {
		builtin := s.getBuiltinSkillsForAgent(id)
		learned := s.getLearnedSkillsForAgent(id)
		if len(builtin) == 0 && len(learned) == 0 {
			continue
		}
		result = append(result, agentSkillsResponse{
			AgentID:  id,
			Builtin:  builtin,
			Learned:  learned,
			AllCount: len(builtin) + len(learned),
		})
	}

	writeOK(w, result)
}

func (s *APIServer) handleSkillByName(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/skills/")
	parts := strings.SplitN(rest, "/", 2)

	agentID := r.URL.Query().Get("agent_id")
	name := parts[0]
	if len(parts) == 2 && agentID == "" {
		agentID = parts[0]
		name = parts[1]
	}
	name = strings.TrimSuffix(name, "/")

	if agentID == "" || name == "" {
		writeError(w, http.StatusBadRequest, "agent_id and skill name required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Try learned skill first
		if s.cfg.IsLearningEnabled() {
			repo, err := s.getSkillRepo(agentID)
			if err == nil {
				skill, err := repo.GetSkill(r.Context(), name)
				if err == nil && skill != nil {
					writeOK(w, skill)
					return
				}
			}
		}

		// Try builtin skill
		workspace := s.cfg.ResolveAgentWorkspace(agentID)
		loader := agent.NewSkillsLoader(workspace)
		content := loader.LoadSkill(name)
		if content != "" {
			writeOK(w, map[string]any{
				"name":        name,
				"description": extractDescription(content),
				"content":     content,
				"source":      "builtin",
			})
			return
		}

		writeError(w, http.StatusNotFound, "skill not found: "+name)

	case http.MethodPut:
		if !s.cfg.IsLearningEnabled() {
			writeError(w, http.StatusServiceUnavailable, "learning not enabled")
			return
		}
		repo, err := s.getSkillRepo(agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		var skill learning.Skill
		if err := readJSON(r, &skill); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		skill.Name = name
		if err := repo.SaveSkill(r.Context(), &skill); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		saved, _ := repo.GetSkill(r.Context(), name)
		if saved != nil {
			writeOK(w, saved)
		} else {
			writeOK(w, skill)
		}

	case http.MethodDelete:
		if !s.cfg.IsLearningEnabled() {
			writeError(w, http.StatusServiceUnavailable, "learning not enabled")
			return
		}
		repo, err := s.getSkillRepo(agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := repo.DeleteSkill(r.Context(), name); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeOK(w, map[string]string{"status": "deleted", "name": name})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func extractDescription(content string) string {
	if content == "" {
		return ""
	}
	// Try to extract from frontmatter description field
	if strings.HasPrefix(content, "---") {
		endIdx := strings.Index(content[3:], "---")
		if endIdx > 0 {
			frontmatter := content[3 : 3+endIdx]
			for _, line := range strings.Split(frontmatter, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "description:") {
					desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
					return strings.Trim(desc, `"'`)
				}
			}
		}
	}
	// Fallback: first non-empty, non-header line
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 120 {
			return line[:120] + "..."
		}
		return line
	}
	return ""
}
