package fakeapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func call(t *testing.T, s *Server, method, path, key string) (int, Object) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, s.URL()+path, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var body Object
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body
}

func TestAuthentication(t *testing.T) {
	t.Parallel()
	s := New(t)
	if status, _ := call(t, s, http.MethodGet, "/v1/organizations", ""); status != http.StatusUnauthorized {
		t.Fatalf("anonymous call answered %d", status)
	}
	if status, _ := call(t, s, http.MethodGet, "/v1/organizations", "sk-onetsolutions-wrong"); status != http.StatusUnauthorized {
		t.Fatalf("wrong key answered %d", status)
	}
	if status, _ := call(t, s, http.MethodGet, "/v1/organizations", s.APIKey()); status != http.StatusOK {
		t.Fatalf("valid key answered %d", status)
	}
}

func TestUnknownRouteIsNotImplemented(t *testing.T) {
	t.Parallel()
	s := New(t)
	status, body := call(t, s, http.MethodGet, "/v1/nothing", s.APIKey())
	if status != http.StatusNotImplemented || body["code"] != "not_implemented" {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if got := s.Requests(); len(got) != 1 || got[0].Template != "" {
		t.Fatalf("requests = %+v", got)
	}
}

type fatalRecorder struct {
	testing.TB
	message string
}

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.message = fmt.Sprintf(format, args...)
}

func TestRoutesOutsideTheContractAreRefused(t *testing.T) {
	t.Parallel()
	s := New(t)
	recorder := &fatalRecorder{TB: t}
	s.t = recorder
	s.handle(http.MethodPatch, "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys/{id}",
		func(http.ResponseWriter, *http.Request) {})
	if !strings.Contains(recorder.message, "not an operation of schema/openapi.json") {
		t.Fatalf("message = %q", recorder.message)
	}
}

func TestPagination(t *testing.T) {
	t.Parallel()
	s := New(t)
	scope := s.Scope()
	for range 25 {
		s.SeedSSHKey(scope, nil)
	}
	path := fmt.Sprintf("/v1/organizations/%s/projects/%s/compute/ssh-keys", scope.OrganizationID, scope.ProjectID)
	status, body := call(t, s, http.MethodGet, path+"?page=2&page_size=10", s.APIKey())
	if status != http.StatusOK || body["count"] != float64(25) || body["total_pages"] != float64(3) ||
		len(body.List("results")) != 10 || body["next"] == nil || body["previous"] == nil {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if status, _ := call(t, s, http.MethodGet, path+"?page=4&page_size=10", s.APIKey()); status != http.StatusNotFound {
		t.Fatalf("page past the end answered %d", status)
	}
}

func TestProjectScopeIsEnforced(t *testing.T) {
	t.Parallel()
	s := New(t)
	scope := s.Scope()
	key := s.SeedSSHKey(scope, nil)
	other := s.SeedProject(scope.OrganizationID, "Other")
	path := fmt.Sprintf("/v1/organizations/%s/projects/%s/compute/ssh-keys/%s", scope.OrganizationID, other["id"], key["id"])
	if status, _ := call(t, s, http.MethodGet, path, s.APIKey()); status != http.StatusNotFound {
		t.Fatalf("a key read through another project answered %d", status)
	}
	if status, _ := call(t, s, http.MethodDelete, path, s.APIKey()); status != http.StatusNotFound {
		t.Fatalf("a key deleted through another project answered %d", status)
	}
	if !s.Exists(KindSSHKey, key.ID()) {
		t.Fatal("the key was deleted through another project")
	}
}

func TestAnonymousRoutes(t *testing.T) {
	t.Parallel()
	s := &Server{t: t, mux: http.NewServeMux(), apiKey: "sk-onetsolutions-k", injections: map[string][]*Injection{}}
	s.handle(http.MethodGet, "/v1/compute/sizes", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []Object{})
	}, anonymous)
	server := httptest.NewServer(s.mux)
	t.Cleanup(server.Close)
	s.http = server
	if status, _ := call(t, s, http.MethodGet, "/v1/compute/sizes", ""); status != http.StatusOK {
		t.Fatalf("anonymous call answered %d", status)
	}
	if status, _ := call(t, s, http.MethodGet, "/v1/compute/sizes", "sk-onetsolutions-other"); status != http.StatusUnauthorized {
		t.Fatalf("a wrong key answered %d", status)
	}
}

func TestInjection(t *testing.T) {
	t.Parallel()
	s := New(t)
	s.Inject(http.MethodGet, "/v1/organizations", Injection{Status: http.StatusServiceUnavailable, Times: 2})
	for i := range 2 {
		if status, _ := call(t, s, http.MethodGet, "/v1/organizations", s.APIKey()); status != http.StatusServiceUnavailable {
			t.Fatalf("call %d answered %d", i, status)
		}
	}
	if status, _ := call(t, s, http.MethodGet, "/v1/organizations", s.APIKey()); status != http.StatusOK {
		t.Fatalf("third call answered %d", status)
	}
	if n := s.CountRequests(http.MethodGet, "/v1/organizations"); n != 3 {
		t.Fatalf("counted %d requests", n)
	}
}

func TestTaskEngine(t *testing.T) {
	t.Parallel()
	s := New(t, WithTaskPolls(2))
	scope := s.Scope()
	instance := s.SeedInstance(scope, nil)
	instanceID := instance.ID()
	completed := false
	task := s.newInstanceTask(scope, instanceID, "stop_vps", Object{}, func() {
		s.Mutate(KindInstance, instanceID, func(o Object) { o["state_sync"] = Object{"vm_status": "stopped"} })
		completed = true
	})
	path := fmt.Sprintf("/v1/organizations/%s/projects/%s/compute/instances/%s/tasks/%s",
		scope.OrganizationID, scope.ProjectID, instanceID, task["id"])

	if _, body := call(t, s, http.MethodGet, path, s.APIKey()); body["status"] != "running" || completed {
		t.Fatalf("first poll: %v", body["status"])
	}
	if _, body := call(t, s, http.MethodGet, path, s.APIKey()); body["status"] != "completed" || !completed {
		t.Fatalf("second poll: %v", body["status"])
	}
	stored, _ := s.Get(KindInstance, instanceID)
	if stored.Map("state_sync")["vm_status"] != "stopped" {
		t.Fatal("onComplete did not run")
	}
}

func TestAfterPolls(t *testing.T) {
	t.Parallel()
	s := New(t)
	scope := s.Scope()
	instance := s.SeedInstance(scope, Object{"status": "provisioning"})
	id := instance.ID()
	s.afterPolls(KindInstance, id, 2, func(o Object) { o["status"] = "active" })
	first, _ := s.lookup(KindInstance, scope, "", id)
	second, _ := s.lookup(KindInstance, scope, "", id)
	if first["status"] != "provisioning" || second["status"] != "active" {
		t.Fatalf("statuses %v then %v", first["status"], second["status"])
	}
}

func TestSeedsServeTheAPIShape(t *testing.T) {
	t.Parallel()
	s := New(t)
	scope := s.Scope()
	instance := s.SeedInstance(scope, nil)
	if instance["primary_ipv4"] == nil || len(instance.List("ip_addresses")) != 1 {
		t.Fatalf("instance = %v", instance)
	}
	rules := s.SeedPlatformFirewallRules(scope, instance.ID())
	if rules[0]["source_label"] != ControlPlaneLabel || !strings.HasPrefix(rules[1].String("name"), ManagedRulePrefix) {
		t.Fatalf("rules = %v", rules)
	}
	for kind, minimum := range map[Kind]int{KindSize: 10, KindPublicImage: 10, KindControlPanel: 5, KindHostingPlan: 1} {
		if n := len(s.All(kind)); n < minimum {
			t.Errorf("%d %s fixtures", n, kind)
		}
	}
}

// DELETE on an instance cancels it at the end of its period, idempotently: 202 with the instance
// every time, the first request kept, the instance still listed; revoke clears the request.
func TestInstanceDeleteCancels(t *testing.T) {
	t.Parallel()
	s := New(t)
	scope := s.Scope()
	instance := s.SeedInstance(scope, Object{"next_billing_date": "2026-12-01T00:00:00Z", "locked": true})
	path := fmt.Sprintf("/v1/organizations/%s/projects/%s/compute/instances/%s", scope.OrganizationID, scope.ProjectID, instance.ID())

	status, first := call(t, s, http.MethodDelete, path, s.APIKey())
	if status != http.StatusAccepted || first["cancellation_requested_at"] == nil || first["billing_end_date"] != "2026-12-01T00:00:00Z" ||
		first["cancellation_reason_code"] != "no_longer_needed" || first["cancellation_reason"] != DeleteInstanceDefaultReason {
		t.Fatalf("first DELETE: %d %v", status, first)
	}
	status, second := call(t, s, http.MethodDelete, path, s.APIKey())
	if status != http.StatusAccepted || second["cancellation_requested_at"] != first["cancellation_requested_at"] {
		t.Fatalf("second DELETE: %d %v", status, second)
	}
	if stored, ok := s.Get(KindInstance, instance.ID()); !ok || stored["is_active"] != true {
		t.Fatalf("the instance is gone or inactive after DELETE: %v", stored)
	}
	if status, revoked := call(t, s, http.MethodPost, path+"/revoke-cancellation", s.APIKey()); status != http.StatusOK ||
		revoked["cancellation_requested_at"] != nil || revoked["billing_end_date"] != nil {
		t.Fatalf("revoke: %d %v", status, revoked)
	}
	if status, _ := call(t, s, http.MethodPost, path+"/revoke-cancellation", s.APIKey()); status != http.StatusBadRequest {
		t.Fatalf("revoking nothing answered %d", status)
	}
	missing := fmt.Sprintf("/v1/organizations/%s/projects/%s/compute/instances/%s", scope.OrganizationID, scope.ProjectID,
		"00000000-0000-4000-8000-000000000000")
	if status, _ := call(t, s, http.MethodDelete, missing, s.APIKey()); status != http.StatusNotFound {
		t.Fatalf("DELETE of a missing instance answered %d", status)
	}
}

// list_instances keeps only the instance of ?order_id= and refuses a malformed one; under
// WithoutOrderLink it ignores the filter and serves no order_id.
func TestInstanceListOrderFilter(t *testing.T) {
	t.Parallel()
	const orderID = "55555555-5555-4555-8555-555555555555"
	for _, legacy := range []bool{false, true} {
		var opts []Option
		if legacy {
			opts = append(opts, WithoutOrderLink())
		}
		s := New(t, opts...)
		scope := s.Scope()
		s.SeedInstance(scope, Object{"order_id": orderID})
		s.SeedInstance(scope, nil)
		path := fmt.Sprintf("/v1/organizations/%s/projects/%s/compute/instances", scope.OrganizationID, scope.ProjectID)

		status, page := call(t, s, http.MethodGet, path+"?order_id="+orderID, s.APIKey())
		results, _ := page["results"].([]any)
		want := 1
		if legacy {
			want = 2
		}
		if status != http.StatusOK || len(results) != want {
			t.Fatalf("legacy=%v: %d, %d results, want %d", legacy, status, len(results), want)
		}
		first, _ := results[0].(map[string]any)
		if _, carried := first["order_id"]; carried == legacy {
			t.Fatalf("legacy=%v: order_id carried = %v", legacy, carried)
		}
		if status, _ := call(t, s, http.MethodGet, path+"?order_id=not-a-uuid", s.APIKey()); !legacy && status != http.StatusBadRequest {
			t.Fatalf("a malformed order_id answered %d", status)
		}
	}
}
