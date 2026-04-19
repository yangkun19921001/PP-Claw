package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"
)

// structToMap converts a Go struct to map[string]any using yaml tags as keys.
// This ensures JSON output uses snake_case keys matching the YAML config format,
// since the config structs only have yaml tags (no json tags).
func structToMap(v any) any {
	return reflectToMap(reflect.ValueOf(v))
}

func reflectToMap(v reflect.Value) any {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		m := make(map[string]any)
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}

			tag := field.Tag.Get("yaml")
			if tag == "-" {
				continue
			}

			parts := strings.Split(tag, ",")
			name := parts[0]

			isInline := false
			for _, p := range parts[1:] {
				if strings.TrimSpace(p) == "inline" {
					isInline = true
				}
			}

			fv := v.Field(i)

			if isInline {
				if sub, ok := reflectToMap(fv).(map[string]any); ok {
					for k, sv := range sub {
						m[k] = sv
					}
				}
				continue
			}

			if name == "" {
				name = strings.ToLower(field.Name)
			}

			m[name] = reflectToMap(fv)
		}
		return m

	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		m := make(map[string]any)
		for _, key := range v.MapKeys() {
			m[fmt.Sprint(key.Interface())] = reflectToMap(v.MapIndex(key))
		}
		return m

	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		arr := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			arr[i] = reflectToMap(v.Index(i))
		}
		return arr

	default:
		if v.CanInterface() {
			if d, ok := v.Interface().(time.Duration); ok {
				return d.String()
			}
			return v.Interface()
		}
		return nil
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter, v any) {
	writeJSON(w, http.StatusOK, v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func pathParam(r *http.Request, prefix string) string {
	p := strings.TrimPrefix(r.URL.Path, prefix)
	p = strings.TrimSuffix(p, "/")
	if idx := strings.Index(p, "/"); idx >= 0 {
		return p[:idx]
	}
	return p
}

func pathSuffix(r *http.Request, prefix string) string {
	return strings.TrimPrefix(r.URL.Path, prefix)
}
