package client

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Route is one API operation: a method and an OpenAPI path template such as
// "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys/{id}".
//
// Routes can only be built by newRoute, which records them: the contract test checks every
// recorded route against schema/openapi.json, so a client method cannot reach an operation
// outside the published contract without that test failing.
type Route struct {
	method    string
	template  string
	retrySafe bool
}

// Method returns the HTTP method of the route.
func (r Route) Method() string { return r.method }

// Template returns the OpenAPI path template of the route.
func (r Route) Template() string { return r.template }

// RetrySafe reports whether a failed attempt may be repeated: true for idempotent methods and for
// the POSTs declared with newRetrySafeRoute.
func (r Route) RetrySafe() bool { return r.retrySafe }

var (
	routesMu sync.Mutex
	routes   = map[string]Route{}
)

// newRoute declares a route. Call it from a package-level variable so the route is recorded
// before any test runs.
func newRoute(method, template string) Route {
	return register(Route{method: method, template: template, retrySafe: idempotent(method)})
}

// newRetrySafeRoute declares a POST that has no side effect when repeated (a preview, a
// computation), so transient failures may be retried like a GET.
func newRetrySafeRoute(template string) Route {
	return register(Route{method: http.MethodPost, template: template, retrySafe: true})
}

func register(r Route) Route {
	routesMu.Lock()
	defer routesMu.Unlock()
	key := r.method + " " + r.template
	if existing, ok := routes[key]; ok && existing != r {
		panic(fmt.Sprintf("route %s declared twice with different retry semantics", key))
	}
	routes[key] = r
	return r
}

// Routes returns every declared route, sorted by method and template.
func Routes() []Route {
	routesMu.Lock()
	defer routesMu.Unlock()
	list := make([]Route, 0, len(routes))
	for _, r := range routes {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].method+" "+list[i].template < list[j].method+" "+list[j].template
	})
	return list
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

// Params holds the values of a route's path parameters, keyed by the names of the template.
type Params map[string]string

// Scope is the organization and project a project-level object lives in.
type Scope struct {
	OrganizationID string
	ProjectID      string
}

// Params returns the path parameters of the scope, ready to be extended with the object's own.
func (s Scope) Params(extra ...string) Params {
	p := Params{"organization_id": s.OrganizationID, "project_id": s.ProjectID}
	for i := 0; i+1 < len(extra); i += 2 {
		p[extra[i]] = extra[i+1]
	}
	return p
}

// expand substitutes every {name} of the template. A missing or empty value is an error rather
// than a request to a different path ("/organizations//projects").
func (r Route) expand(params Params) (string, error) {
	var b strings.Builder
	used := 0
	rest := r.template
	for {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			b.WriteString(rest)
			break
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			return "", fmt.Errorf("route %s: unterminated parameter", r.template)
		}
		name := rest[start+1 : start+end]
		value, ok := params[name]
		if !ok || value == "" {
			return "", fmt.Errorf("route %s: missing value for path parameter %q", r.template, name)
		}
		b.WriteString(rest[:start])
		b.WriteString(url.PathEscape(value))
		used++
		rest = rest[start+end+1:]
	}
	if used != len(params) {
		return "", fmt.Errorf("route %s: %d path parameters given, %d used", r.template, len(params), used)
	}
	return b.String(), nil
}
