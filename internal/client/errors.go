package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const maxRawErrorLength = 512

// APIError is a non-2xx answer of the API, decoded from any of its error shapes: domain errors
// ({detail, code, error, context}), validation errors ({"field": ["message"]}) and plain
// errors ({"detail": "..."}).
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Code       string
	Detail     string
	Context    map[string]any
	// Fields maps a request field (dotted for nested ones, "non_field_errors" for the object
	// itself) to the API's messages about it.
	Fields map[string][]string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	if e.Code != "" {
		fmt.Fprintf(&b, " (%s)", e.Code)
	}
	if e.Detail != "" {
		b.WriteString(": ")
		b.WriteString(e.Detail)
	}
	if fields := e.FieldMessages(); len(fields) > 0 {
		b.WriteString(": ")
		b.WriteString(strings.Join(fields, "; "))
	}
	return b.String()
}

// FieldMessages renders the field errors as "field: message", sorted by field.
func (e *APIError) FieldMessages() []string {
	names := make([]string, 0, len(e.Fields))
	for name := range e.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines []string
	for _, name := range names {
		for _, message := range e.Fields[name] {
			lines = append(lines, name+": "+message)
		}
	}
	return lines
}

// AsAPIError unwraps err into an *APIError.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// IsNotFound reports whether err is an API 404: the object is gone.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsForbidden reports whether err is an API 403: the caller may not do this, or not on the object
// as it is (a change under an instance that is no longer active).
func IsForbidden(err error) bool { return hasStatus(err, http.StatusForbidden) }

// IsConflict reports whether err is an API 409: wrong state or an operation in progress.
func IsConflict(err error) bool { return hasStatus(err, http.StatusConflict) }

// HasCode reports whether err is an API error carrying the given machine-readable code.
func HasCode(err error, code string) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Code == code
}

func hasStatus(err error, status int) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.StatusCode == status
}

func parseAPIError(status int, method, path string, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status, Method: method, Path: path}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		apiErr.Detail = http.StatusText(status)
		return apiErr
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err == nil {
		apiErr.fromObject(object)
		if apiErr.Detail == "" && len(apiErr.Fields) == 0 {
			apiErr.Detail = http.StatusText(status)
		}
		return apiErr
	}

	var messages []string
	if err := json.Unmarshal(trimmed, &messages); err == nil {
		apiErr.Detail = strings.Join(messages, "; ")
		return apiErr
	}

	if trimmed[0] == '<' {
		apiErr.Detail = http.StatusText(status)
		return apiErr
	}
	raw := string(trimmed)
	if len(raw) > maxRawErrorLength {
		raw = raw[:maxRawErrorLength] + "…"
	}
	apiErr.Detail = raw
	return apiErr
}

func (e *APIError) fromObject(object map[string]json.RawMessage) {
	for key, raw := range object {
		switch key {
		case "detail":
			var detail string
			if json.Unmarshal(raw, &detail) == nil {
				e.Detail = detail
				continue
			}
			e.addFieldErrors("", raw)
		case "error":
			var legacy string
			if json.Unmarshal(raw, &legacy) == nil && e.Detail == "" {
				e.Detail = legacy
			}
		case "code":
			var code string
			if json.Unmarshal(raw, &code) == nil {
				e.Code = code
			}
		case "context":
			var context map[string]any
			if json.Unmarshal(raw, &context) == nil {
				e.Context = context
			}
		default:
			e.addFieldErrors(key, raw)
		}
	}
}

func (e *APIError) addFieldErrors(field string, raw json.RawMessage) {
	var message string
	if json.Unmarshal(raw, &message) == nil {
		e.addField(field, message)
		return
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for index, item := range list {
			var text string
			if json.Unmarshal(item, &text) == nil {
				e.addField(field, text)
				continue
			}
			e.addFieldErrors(join(field, fmt.Sprint(index)), item)
		}
		return
	}
	var nested map[string]json.RawMessage
	if json.Unmarshal(raw, &nested) == nil {
		for key, value := range nested {
			e.addFieldErrors(join(field, key), value)
		}
	}
}

func (e *APIError) addField(field, message string) {
	if field == "" {
		field = "non_field_errors"
	}
	if e.Fields == nil {
		e.Fields = map[string][]string{}
	}
	e.Fields[field] = append(e.Fields[field], message)
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
