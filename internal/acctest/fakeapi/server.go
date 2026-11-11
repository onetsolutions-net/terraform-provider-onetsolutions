// Package fakeapi is an in-process fake of the OnetSolutions public API for the provider tests:
// in-memory objects per organization and project, tasks that complete after a few polls, error
// injection, and Seed helpers so a test can create objects owned by another area.
//
// Each area registers its routes in its own file (account.go, base.go, catalog.go, compute.go,
// network.go, dns.go, backup.go, domain_order.go, hosting.go, …) through Server.handle, which
// refuses any route that is not an operation of schema/openapi.json or of the documented
// allow-list.
package fakeapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/contract"
)

// DefaultTaskPolls is how many reads of a task it takes to complete.
const DefaultTaskPolls = 2

// Server is a running fake API. Its methods are safe for concurrent use.
type Server struct {
	t      testing.TB
	http   *httptest.Server
	mux    *http.ServeMux
	apiKey string
	scope  Scope

	mu          sync.Mutex
	store       *store
	taskPolls   int
	tasks       map[string]*taskState
	failTasks   map[string][]string
	transitions map[string][]*transition
	pollCounts  map[string]int
	injections  map[string][]*Injection
	requests    []Request

	unpaidResizes      bool
	unpaidOrders       bool
	installedStopped   bool
	withoutOrderLink   bool
	powerSyncDelay     int
	pendingResizes     map[string]bool
	unpaidInstanceRuns map[string]unpaidOrder
	// powerStatus holds Instance.power_status where it differs from what state_sync reads; the
	// API never serves it, but checks the power actions against it.
	powerStatus map[string]string
	syncCatchUp map[string]int

	domainOrders *domainOrders
	hosting      hostingState
}

// Option configures New.
type Option func(*Server)

// WithTaskPolls sets how many reads of a task it takes to complete (DefaultTaskPolls otherwise).
func WithTaskPolls(n int) Option {
	return func(s *Server) { s.taskPolls = n }
}

// WithUnpaidResizes leaves every resize order unpaid: the size never changes, and a second resize
// of the same instance is refused with 409 upgrade_awaiting_payment, like the API.
func WithUnpaidResizes() Option {
	return func(s *Server) { s.unpaidResizes = true }
}

// WithUnpaidOrders leaves every instance order unpaid, as when the account balance does not
// cover it: the answer carries amount_due > 0, the invoice and its payment link, and no instance
// is created, since the API creates it only once the order is paid (PayOrder).
func WithUnpaidOrders() Option {
	return func(s *Server) { s.unpaidOrders = true }
}

// WithoutOrderLink serves the instances as an older API version does: no order_id field,
// and list_instances ignores ?order_id=, listing every instance.
func WithoutOrderLink() Option {
	return func(s *Server) { s.withoutOrderLink = true }
}

// WithPowerSyncDelay leaves a new instance's power state unsynced (state_sync null) for polls
// reads of the instance after its installation, as the API serves it until the first sync.
func WithPowerSyncDelay(polls int) Option {
	return func(s *Server) { s.powerSyncDelay = polls }
}

// WithInstalledStopped leaves a new instance's synced power state "stopped" once it is installed,
// as a power state synced late reads.
func WithInstalledStopped() Option {
	return func(s *Server) { s.installedStopped = true }
}

// New starts a fake API with one organization ("Acme") holding one project ("Default"), the
// catalog captured from the real API (sizes, public images, control panels,
// hosting plans), and every area's routes. It stops when the test ends.
func New(t testing.TB, opts ...Option) *Server {
	t.Helper()
	s := &Server{
		t:           t,
		mux:         http.NewServeMux(),
		apiKey:      "sk-onetsolutions-test" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		store:       newStore(),
		taskPolls:   DefaultTaskPolls,
		tasks:       map[string]*taskState{},
		failTasks:   map[string][]string{},
		transitions: map[string][]*transition{},
		pollCounts:  map[string]int{},
		injections:  map[string][]*Injection{},

		pendingResizes:     map[string]bool{},
		unpaidInstanceRuns: map[string]unpaidOrder{},
		powerStatus:        map[string]string{},
		syncCatchUp:        map[string]int{},
	}
	for _, opt := range opts {
		opt(s)
	}

	org := s.SeedOrganization("Acme")
	project := s.SeedProject(org.ID(), "Default")
	s.scope = Scope{OrganizationID: org.ID(), ProjectID: project.ID()}
	s.seedCatalogue()

	registerTasks(s)
	registerAccount(s)
	registerBase(s)
	registerCatalog(s)
	registerCompute(s)
	registerNetwork(s)
	registerDNS(s)
	registerBackup(s)
	registerDomainOrders(s)
	registerHosting(s)
	registerHostingMailDB(s)
	registerHostingWeb(s)
	s.mux.HandleFunc("/", s.notImplemented)

	s.http = httptest.NewServer(s.mux)
	t.Cleanup(s.http.Close)
	return s
}

// URL is the base URL to give the provider as api_url.
func (s *Server) URL() string { return s.http.URL }

// APIKey is the only API key the fake accepts.
func (s *Server) APIKey() string { return s.apiKey }

// Scope is the default organization and project.
func (s *Server) Scope() Scope { return s.scope }

// Request is one request the fake received.
type Request struct {
	Method string
	Path   string
	// Template is the OpenAPI path template that served it, empty when no route matched.
	Template string
}

// Requests returns every request received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// CountRequests counts the requests received on one route.
func (s *Server) CountRequests(method, template string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.requests {
		if r.Method == method && r.Template == template {
			n++
		}
	}
	return n
}

// Injection makes the next requests on a route fail.
type Injection struct {
	Status int
	// Body defaults to {"detail": <status text>, "code": "injected"}.
	Body Object
	// RetryAfter sets the Retry-After header, in seconds.
	RetryAfter string
	// Times is how many requests fail; 0 means 1.
	Times int
	// AfterHandling lets the request take effect before the failure is answered, like a
	// connection lost or a gateway timeout after the API committed it.
	AfterHandling bool
}

// Inject makes the next requests on method+template fail as described.
func (s *Server) Inject(method, template string, inj Injection) {
	if inj.Times <= 0 {
		inj.Times = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + template
	s.injections[key] = append(s.injections[key], &inj)
}

type routeOption int

const (
	// anonymous routes answer without an API key, like the catalog.
	anonymous routeOption = iota + 1
)

// handle registers an area's route. template is the OpenAPI path template, e.g.
// "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys/{id}"; the
// handler reads the parameters with r.PathValue. A route outside the contract fails the test.
func (s *Server) handle(method, template string, handler http.HandlerFunc, opts ...routeOption) {
	s.t.Helper()
	if err := contract.Check(method, template); err != nil {
		s.t.Fatalf("fakeapi: %v", err)
	}
	public := false
	for _, opt := range opts {
		if opt == anonymous {
			public = true
		}
	}
	s.mux.HandleFunc(method+" "+template, func(w http.ResponseWriter, r *http.Request) {
		s.record(r, template)
		if !s.authorized(r, public) {
			writeError(w, http.StatusUnauthorized, "Authentication credentials were not provided.", "not_authenticated")
			return
		}
		inj, ok := s.nextInjection(method, template)
		if !ok {
			handler(w, r)
			return
		}
		if inj.AfterHandling {
			handler(httptest.NewRecorder(), r)
		}
		writeInjected(w, inj)
	})
}

func (s *Server) record(r *http.Request, template string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Template: template})
}

func (s *Server) authorized(r *http.Request, public bool) bool {
	header := r.Header.Get("Authorization")
	if header == "" {
		return public
	}
	return header == "Bearer "+s.apiKey
}

func (s *Server) nextInjection(method, template string) (Injection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + template
	queue := s.injections[key]
	if len(queue) == 0 {
		return Injection{}, false
	}
	inj := *queue[0]
	queue[0].Times--
	if queue[0].Times <= 0 {
		s.injections[key] = queue[1:]
	}
	return inj, true
}

func writeInjected(w http.ResponseWriter, inj Injection) {
	if inj.RetryAfter != "" {
		w.Header().Set("Retry-After", inj.RetryAfter)
	}
	body := inj.Body
	if body == nil {
		body = Object{"detail": http.StatusText(inj.Status), "code": "injected"}
	}
	writeJSON(w, inj.Status, body)
}

func (s *Server) notImplemented(w http.ResponseWriter, r *http.Request) {
	s.record(r, "")
	writeError(w, http.StatusNotImplemented,
		"fakeapi: no handler for "+r.Method+" "+r.URL.Path+"; register it in the area's file", "not_implemented")
}
