package manage

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (a *API) handleResource(w http.ResponseWriter, r *http.Request) {
	resource, id := parsePath(r.URL.Path)
	if resource == "" {
		http.Error(w, "resource is required", http.StatusBadRequest)
		return
	}

	var (
		result any
		err    error
	)
	switch r.Method {
	case http.MethodGet:
		if id == "" {
			result, err = a.wrapper.List(r.Context(), resource)
		} else {
			result, err = a.wrapper.Get(r.Context(), resource, id)
		}
	case http.MethodPost, http.MethodPut:
		body, readErr := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadRequest)
			return
		}
		result, err = a.wrapper.UpsertJSON(r.Context(), resource, body)
	case http.MethodDelete:
		if id == "" {
			http.Error(w, "resource id is required", http.StatusBadRequest)
			return
		}
		err = a.wrapper.Delete(r.Context(), resource, id)
		result = map[string]any{"deleted": true}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func parsePath(path string) (string, string) {
	trimmed := strings.TrimPrefix(path, "/manage/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
