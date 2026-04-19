package api

import (
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/yangkun19921001/PP-Claw/config"
	"gopkg.in/yaml.v3"
)

func (s *APIServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w, r)
	case http.MethodPut:
		s.handlePutConfig(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *APIServer) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	raw, ok := structToMap(s.cfg).(map[string]any)
	if !ok {
		writeError(w, http.StatusInternalServerError, "config serialization error")
		return
	}
	sanitizeSecrets(raw)
	writeOK(w, raw)
}

func (s *APIServer) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var incoming map[string]any
	if err := readJSON(r, &incoming); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Reload from disk to pick up any manual YAML edits before merging
	if freshCfg, err := config.Load(s.cfgPath); err == nil {
		*s.cfg = *freshCfg
	}

	currentMap, _ := structToMap(s.cfg).(map[string]any)
	for key, val := range incoming {
		currentMap[key] = val
	}

	fixDurationFields(currentMap)

	yamlData, err := yaml.Marshal(currentMap)
	if err != nil {
		writeError(w, http.StatusBadRequest, "config conversion error: "+err.Error())
		return
	}

	var newCfg config.Config
	if err := yaml.Unmarshal(yamlData, &newCfg); err != nil {
		writeError(w, http.StatusBadRequest, "config parse error: "+err.Error())
		return
	}

	restoreSecrets(&newCfg, s.cfg)

	if err := config.Save(&newCfg, s.cfgPath); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed: "+err.Error())
		return
	}

	*s.cfg = newCfg
	writeOK(w, map[string]string{"status": "saved"})
}

func (s *APIServer) handleConfigSection(w http.ResponseWriter, r *http.Request) {
	section := pathParam(r, "/api/v1/config/")
	switch r.Method {
	case http.MethodGet:
		s.getConfigSection(w, section)
	case http.MethodPut:
		s.putConfigSection(w, r, section)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *APIServer) getConfigSection(w http.ResponseWriter, section string) {
	raw, ok := structToMap(s.cfg).(map[string]any)
	if !ok {
		writeError(w, http.StatusInternalServerError, "config serialization error")
		return
	}

	val, exists := raw[section]
	if !exists {
		writeError(w, http.StatusNotFound, "section not found: "+section)
		return
	}
	if m, ok := val.(map[string]any); ok {
		sanitizeSecrets(m)
	}
	writeOK(w, val)
}

func (s *APIServer) putConfigSection(w http.ResponseWriter, r *http.Request, section string) {
	var sectionData any
	if err := readJSON(r, &sectionData); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Reload from disk to pick up any manual YAML edits before merging
	if freshCfg, err := config.Load(s.cfgPath); err == nil {
		*s.cfg = *freshCfg
	}

	currentMap, _ := structToMap(s.cfg).(map[string]any)
	currentMap[section] = sectionData
	fixDurationFields(currentMap)

	yamlData, err := yaml.Marshal(currentMap)
	if err != nil {
		writeError(w, http.StatusBadRequest, "config conversion error: "+err.Error())
		return
	}

	var newCfg config.Config
	if err := yaml.Unmarshal(yamlData, &newCfg); err != nil {
		writeError(w, http.StatusBadRequest, "parse error: "+err.Error())
		return
	}

	restoreSecrets(&newCfg, s.cfg)

	if err := config.Save(&newCfg, s.cfgPath); err != nil {
		writeError(w, http.StatusInternalServerError, "save failed: "+err.Error())
		return
	}
	*s.cfg = newCfg
	writeOK(w, map[string]string{"status": "saved", "section": section})
}

var durationFieldNames = map[string]bool{
	"flush_interval":  true,
	"max_duration":    true,
	"update_interval": true,
}

func fixDurationFields(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			fixDurationFields(val)
		case []any:
			for _, item := range val {
				if sub, ok := item.(map[string]any); ok {
					fixDurationFields(sub)
				}
			}
		case float64:
			if durationFieldNames[k] && val > 1e6 {
				m[k] = time.Duration(int64(val)).String()
			}
		case int64:
			if durationFieldNames[k] && val > 1e6 {
				m[k] = time.Duration(val).String()
			}
		}
	}
}

func sanitizeSecrets(m map[string]any) {
	secretKeys := []string{"api_key", "app_secret", "secret", "password", "token", "webhook_secret", "encrypt_key", "access_token"}
	for k, v := range m {
		lower := strings.ToLower(k)
		for _, sk := range secretKeys {
			if strings.Contains(lower, sk) {
				if str, ok := v.(string); ok && str != "" {
					if len(str) > 4 {
						m[k] = "****" + str[len(str)-4:]
					} else {
						m[k] = "****"
					}
				}
			}
		}
		if sub, ok := v.(map[string]any); ok {
			sanitizeSecrets(sub)
		}
		if arr, ok := v.([]any); ok {
			for _, item := range arr {
				if sub, ok := item.(map[string]any); ok {
					sanitizeSecrets(sub)
				}
			}
		}
	}
}

func restoreSecrets(newCfg, oldCfg *config.Config) {
	restoreSecretFields(reflect.ValueOf(newCfg).Elem(), reflect.ValueOf(oldCfg).Elem())
}

func restoreSecretFields(newVal, oldVal reflect.Value) {
	if newVal.Kind() == reflect.Ptr {
		if newVal.IsNil() || oldVal.IsNil() {
			return
		}
		restoreSecretFields(newVal.Elem(), oldVal.Elem())
		return
	}
	if newVal.Kind() != reflect.Struct {
		return
	}
	t := newVal.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		nf := newVal.Field(i)
		of := oldVal.Field(i)

		if nf.Kind() == reflect.String {
			lower := strings.ToLower(field.Name)
			isSecret := strings.Contains(lower, "key") ||
				strings.Contains(lower, "secret") ||
				strings.Contains(lower, "password") ||
				strings.Contains(lower, "token")
			if isSecret {
				newStr := nf.String()
				oldStr := of.String()
				if strings.HasPrefix(newStr, "****") || (newStr == "" && oldStr != "") {
					nf.SetString(oldStr)
				}
			}
		}
		if nf.Kind() == reflect.Struct {
			restoreSecretFields(nf, of)
		}
		if nf.Kind() == reflect.Ptr {
			restoreSecretFields(nf, of)
		}
		if nf.Kind() == reflect.Map && nf.Len() > 0 {
			iter := nf.MapRange()
			for iter.Next() {
				key := iter.Key()
				nv := iter.Value()
				if nv.Kind() == reflect.Struct {
					ov := oldVal.Field(i).MapIndex(key)
					if ov.IsValid() {
						restoreMapStructSecrets(nf, key, nv, ov)
					}
				}
			}
		}
	}
}

func restoreMapStructSecrets(m reflect.Value, key, newVal, oldVal reflect.Value) {
	if newVal.Kind() != reflect.Struct {
		return
	}
	modified := reflect.New(newVal.Type()).Elem()
	modified.Set(newVal)
	t := newVal.Type()
	changed := false
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		nf := modified.Field(i)
		of := oldVal.Field(i)
		if nf.Kind() == reflect.String {
			lower := strings.ToLower(field.Name)
			isSecret := strings.Contains(lower, "key") ||
				strings.Contains(lower, "secret") ||
				strings.Contains(lower, "password") ||
				strings.Contains(lower, "token")
			if isSecret {
				newStr := nf.String()
				oldStr := of.String()
				if strings.HasPrefix(newStr, "****") || (newStr == "" && oldStr != "") {
					nf.SetString(oldStr)
					changed = true
				}
			}
		}
	}
	if changed {
		m.SetMapIndex(key, modified)
	}
}
