package fakeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	if body == nil {
		w.WriteHeader(status)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "fakeapi: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// writeError answers a domain error the way the API does: {detail, code, error}.
func writeError(w http.ResponseWriter, status int, detail, code string) {
	writeJSON(w, status, Object{"detail": detail, "code": code, "error": detail})
}

// writeFieldErrors answers one of the API's validation errors: {"field": ["message"]}.
func writeFieldErrors(w http.ResponseWriter, fields map[string][]string) {
	writeJSON(w, http.StatusBadRequest, fields)
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, Object{"detail": "No object matches the given query.", "code": "not_found"})
}

// writePage answers a paginated list with the API's envelope.
func writePage(w http.ResponseWriter, r *http.Request, items []Object) {
	query := r.URL.Query()
	page, pageSize := 1, defaultPageSize
	if raw := query.Get("page_size"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			pageSize = min(n, maxPageSize)
		}
	}
	if raw := query.Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeJSON(w, http.StatusNotFound, Object{"detail": "Invalid page."})
			return
		}
		page = n
	}
	totalPages := max(1, (len(items)+pageSize-1)/pageSize)
	if page > totalPages {
		writeJSON(w, http.StatusNotFound, Object{"detail": "Invalid page."})
		return
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, len(items))

	link := func(target int) any {
		if target < 1 || target > totalPages {
			return nil
		}
		q := url.Values{}
		for key, values := range query {
			q[key] = values
		}
		q.Set("page", strconv.Itoa(target))
		return fmt.Sprintf("http://%s%s?%s", r.Host, r.URL.Path, q.Encode())
	}
	writeJSON(w, http.StatusOK, Object{
		"count":       len(items),
		"next":        link(page + 1),
		"previous":    link(page - 1),
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
		"results":     items[start:end],
	})
}

// decodeBody reads a JSON object body, answering 400 when it is not one.
func decodeBody(w http.ResponseWriter, r *http.Request) (Object, bool) {
	var body Object
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body == nil {
		writeJSON(w, http.StatusBadRequest, Object{"detail": "JSON parse error.", "code": "parse_error"})
		return nil, false
	}
	return body, true
}

// projectScope checks the organization and project of the path exist, answering 404 otherwise.
func (s *Server) projectScope(w http.ResponseWriter, r *http.Request) (Scope, bool) {
	scope := Scope{OrganizationID: r.PathValue("organization_id"), ProjectID: r.PathValue("project_id")}
	if _, ok := s.lookup(KindOrganization, Scope{}, "", scope.OrganizationID); !ok {
		writeNotFound(w)
		return Scope{}, false
	}
	if _, ok := s.lookup(KindProject, Scope{OrganizationID: scope.OrganizationID}, "", scope.ProjectID); !ok {
		writeNotFound(w)
		return Scope{}, false
	}
	return scope, true
}

// stringField returns body[key] when it is a string.
func stringField(body Object, key string) (string, bool) {
	value, ok := body[key].(string)
	return value, ok
}

// trimmedField returns body[key] without surrounding whitespace, like an optional text field trimmed by the API.
func trimmedField(body Object, key string) string {
	value, _ := stringField(body, key)
	return strings.TrimSpace(value)
}

// fieldErrors collects validation messages in the API's {"field": ["message"]} shape.
type fieldErrors map[string][]string

func (f fieldErrors) add(field, message string) { f[field] = append(f[field], message) }

// requireString checks body[key] is a non-blank string of at most maxLength characters and
// returns it without surrounding whitespace, like a required text field trimmed by the API.
func (f fieldErrors) requireString(body Object, key string, maxLength int) string {
	raw, present := body[key]
	if !present || raw == nil {
		f.add(key, "This field is required.")
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		f.add(key, "Not a valid string.")
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" {
		f.add(key, "This field may not be blank.")
		return ""
	}
	if maxLength > 0 && len([]rune(value)) > maxLength {
		f.add(key, fmt.Sprintf("Ensure this field has no more than %d characters.", maxLength))
	}
	return value
}

// withScope resolves the organization/project of the path before calling handler.
func (s *Server) withScope(handler func(http.ResponseWriter, *http.Request, Scope)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := s.projectScope(w, r)
		if !ok {
			return
		}
		handler(w, r, scope)
	}
}

// writeObject answers GET on one object, 404 when it is not in that scope.
func (s *Server) writeObject(w http.ResponseWriter, kind Kind, scope Scope, parentID, id string) {
	object, ok := s.lookup(kind, scope, parentID, id)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, object)
}

// deleteObject answers DELETE on one object with 204, 404 when it is not in that scope.
func (s *Server) deleteObject(w http.ResponseWriter, kind Kind, scope Scope, parentID, id string) {
	if !s.remove(kind, scope, parentID, id) {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) nameTaken(kind Kind, scope Scope, name, exceptID string) bool {
	for _, object := range s.list(kind, scope, "") {
		if object["name"] == name && object["id"] != exceptID {
			return true
		}
	}
	return false
}
