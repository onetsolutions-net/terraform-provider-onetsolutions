package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/contract"
)

const testKey = "sk-onetsolutions-abcDEF123_-xyz"

var (
	testGet  = newRoute(http.MethodGet, "/v1/organizations/{id}")
	testPost = newRoute(http.MethodPost, "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys")
	testList = newRoute(http.MethodGet, "/v1/organizations/{organization_id}/projects")
)

// TestRoutesAreInTheContract is the contract test of plan section 7.3: every route a client
// method can call is an operation of schema/openapi.json or of the documented allow-list.
func TestRoutesAreInTheContract(t *testing.T) {
	t.Parallel()
	all := Routes()
	if len(all) == 0 {
		t.Fatal("no route declared")
	}
	for _, r := range all {
		if err := contract.Check(r.Method(), r.Template()); err != nil {
			t.Errorf("%v (declared in internal/client)", err)
		}
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := New(Config{
		BaseURL:      server.URL,
		APIKey:       testKey,
		UserAgent:    "test-agent",
		RetryMax:     3,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewRejectsInvalidURLs(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"ftp://api.example", "api.onetsolutions.net", "https://"} {
		if _, err := New(Config{BaseURL: raw}); err == nil {
			t.Errorf("New(%q) accepted an invalid URL", raw)
		}
	}
}

func TestParseBaseURL(t *testing.T) {
	t.Parallel()
	for raw, ok := range map[string]bool{
		"https://api.onetsolutions.net": true,
		"http://localhost:8080":         true,
		"http://127.0.0.1:3000/":        true,
		"http://[::1]:3000":             true,
		"http://api.onetsolutions.net":  false,
		"http://10.0.0.5":               false,
		"http://localhost.example.com":  false,
		"ftp://localhost":               false,
	} {
		if _, err := ParseBaseURL(raw); (err == nil) != ok {
			t.Errorf("ParseBaseURL(%q) error = %v, want ok = %v", raw, err, ok)
		}
	}
}

// A redirect is an error: following it would resend the key elsewhere and turn a POST into a GET.
func TestRedirectIsNotFollowed(t *testing.T) {
	t.Parallel()
	var followed atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
	})
	err := c.do(context.Background(), testPost, Params{"organization_id": "o", "project_id": "p"}, map[string]string{"name": "k"}, nil)
	apiErr, ok := AsAPIError(err)
	if !ok || apiErr.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("err = %v, want the 301 as an API error", err)
	}
	if followed.Load() != 0 {
		t.Fatal("the redirect was followed")
	}
}

func TestRequestHeaders(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "test-agent" {
			t.Errorf("User-Agent = %q", got)
		}
		if r.URL.Path != "/v1/organizations/a%2Fb" && r.URL.RawPath != "/v1/organizations/a%2Fb" {
			t.Errorf("path = %q (raw %q), want the parameter escaped", r.URL.Path, r.URL.RawPath)
		}
		_, _ = w.Write([]byte(`{"id":"a/b","name":"Acme","unknown_field":true}`))
	})
	var out Organization
	if err := c.do(context.Background(), testGet, Params{"id": "a/b"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "Acme" {
		t.Fatalf("decoded %+v", out)
	}
}

// A path parameter holding reserved characters is escaped once: "%" is not escaped again, and a
// "/" never adds a path segment.
func TestPathParametersAreEscapedOnce(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/api"} {
		var requestURI string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestURI = r.RequestURI
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(server.Close)
		c, err := New(Config{BaseURL: server.URL + prefix, APIKey: testKey})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.do(context.Background(), testGet, Params{"id": "a/b%c d"}, nil, nil); err != nil {
			t.Fatal(err)
		}
		if want := prefix + "/v1/organizations/a%2Fb%25c%20d"; requestURI != want {
			t.Errorf("request URI %q, want %q", requestURI, want)
		}
	}
}

func TestRouteExpandRefusesMissingParameters(t *testing.T) {
	t.Parallel()
	if _, err := testPost.expand(Params{"organization_id": "o"}); err == nil {
		t.Fatal("expand accepted a missing project_id")
	}
	if _, err := testPost.expand(Params{"organization_id": "o", "project_id": ""}); err == nil {
		t.Fatal("expand accepted an empty project_id")
	}
	if _, err := testGet.expand(Params{"id": "x", "extra": "y"}); err == nil {
		t.Fatal("expand accepted an unused parameter")
	}
}

func TestUserAgent(t *testing.T) {
	t.Parallel()
	got := UserAgent("1.2.3", "1.17.1")
	want := "terraform-provider-onetsolutions/1.2.3 (terraform/1.17.1; +https://registry.terraform.io/providers/onetsolutions-net/onetsolutions)"
	if got != want {
		t.Fatalf("UserAgent = %q", got)
	}
}

func TestThrottledRequestsAreRetriedForEveryMethod(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"k"}`))
	})
	var out SSHKey
	err := c.do(context.Background(), testPost, Params{"organization_id": "o", "project_id": "p"}, SSHKeyCreate{Name: "n"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || out.ID != "k" {
		t.Fatalf("calls = %d, out = %+v", calls.Load(), out)
	}
}

func TestRetryAfterIsHonoured(t *testing.T) {
	t.Parallel()
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"7"}}}
	if got := backoff(time.Millisecond, time.Second, 1, resp); got != 7*time.Second {
		t.Fatalf("backoff = %s, want the 7s of Retry-After", got)
	}
	for attempt := range 10 {
		got := backoff(time.Second, 30*time.Second, attempt, nil)
		if got < 500*time.Millisecond || got > 30*time.Second {
			t.Fatalf("attempt %d: backoff %s outside [0.5s, 30s]", attempt, got)
		}
	}
}

func TestServerErrorsAreRetriedOnlyForRetrySafeRoutes(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		var gets, posts atomic.Int32
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			counter := &gets
			if r.Method == http.MethodPost {
				counter = &posts
			}
			if counter.Add(1) == 1 {
				w.WriteHeader(status)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		})
		if err := c.do(context.Background(), testGet, Params{"id": "x"}, nil, nil); err != nil {
			t.Errorf("GET after %d: %v", status, err)
		}
		err := c.do(context.Background(), testPost, Params{"organization_id": "o", "project_id": "p"}, SSHKeyCreate{}, nil)
		apiErr, ok := AsAPIError(err)
		if !ok || apiErr.StatusCode != status {
			t.Errorf("POST after %d: err = %v, want the API error", status, err)
		}
		if gets.Load() != 2 || posts.Load() != 1 {
			t.Errorf("status %d: %d GETs (want 2), %d POSTs (want 1)", status, gets.Load(), posts.Load())
		}
	}
}

func TestRetrySafePostIsRetried(t *testing.T) {
	t.Parallel()
	preview := newRetrySafeRoute("/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/order-preview")
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := c.do(context.Background(), preview, Params{"organization_id": "o", "project_id": "p"}, testBody{}, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

// testBody is a free-form request body.
type testBody map[string]any

func TestErrorsAfterRetriesExhaustedKeepTheAPIMessage(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"Request was throttled.","code":"throttled"}`))
	})
	err := c.do(context.Background(), testGet, Params{"id": "x"}, nil, nil)
	apiErr, ok := AsAPIError(err)
	if !ok || apiErr.Code != "throttled" || apiErr.Detail != "Request was throttled." {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelledContextStopsRetries(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.do(ctx, testGet, Params{"id": "x"}, nil, nil)
	if !IsCanceled(err) {
		t.Fatalf("err = %v, want a cancellation", err)
	}
}

func TestParseAPIError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		status     int
		body       string
		wantCode   string
		wantDetail string
		wantFields map[string][]string
	}{
		{
			name: "domain error", status: 409,
			body:     `{"detail":"An operation is in progress.","code":"conflict","error":"An operation is in progress.","context":{"task":"t"}}`,
			wantCode: "conflict", wantDetail: "An operation is in progress.",
		},
		{
			name: "legacy error only", status: 400,
			body: `{"error":"Cannot change CIDR block of existing VPC"}`, wantDetail: "Cannot change CIDR block of existing VPC",
		},
		{
			name: "field errors", status: 400,
			body:       `{"name":["SSH key with name 'a' already exists"],"rules":[{"port":["Invalid port."]}],"non_field_errors":["Bad."]}`,
			wantFields: map[string][]string{"name": {"SSH key with name 'a' already exists"}, "rules.0.port": {"Invalid port."}, "non_field_errors": {"Bad."}},
		},
		{
			name: "detail", status: 404, body: `{"detail":"Not found.","code":"not_found"}`,
			wantCode: "not_found", wantDetail: "Not found.",
		},
		{name: "list", status: 400, body: `["first","second"]`, wantDetail: "first; second"},
		{name: "html", status: 502, body: `<html><body>Bad gateway</body></html>`, wantDetail: "Bad Gateway"},
		{name: "empty", status: 503, body: ``, wantDetail: "Service Unavailable"},
		{name: "text", status: 500, body: `boom`, wantDetail: "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := parseAPIError(tc.status, http.MethodGet, "/v1/x", []byte(tc.body))
			if err.StatusCode != tc.status || err.Code != tc.wantCode || err.Detail != tc.wantDetail {
				t.Fatalf("got %+v", err)
			}
			if fmt.Sprint(err.Fields) != fmt.Sprint(tc.wantFields) && (len(err.Fields) != 0 || len(tc.wantFields) != 0) {
				t.Fatalf("fields = %v, want %v", err.Fields, tc.wantFields)
			}
		})
	}
}

func TestErrorHelpers(t *testing.T) {
	t.Parallel()
	notFound := fmt.Errorf("wrapped: %w", &APIError{StatusCode: 404})
	conflict := &APIError{StatusCode: 409, Code: "conflict"}
	if !IsNotFound(notFound) || IsNotFound(conflict) || IsNotFound(errors.New("plain")) {
		t.Fatal("IsNotFound")
	}
	if !IsConflict(conflict) || !HasCode(conflict, "conflict") || HasCode(notFound, "conflict") {
		t.Fatal("IsConflict / HasCode")
	}
	message := (&APIError{StatusCode: 400, Method: "POST", Path: "/p", Fields: map[string][]string{"b": {"2"}, "a": {"1"}}}).Error()
	if message != "POST /p: HTTP 400: a: 1; b: 2" {
		t.Fatalf("Error() = %q", message)
	}
}

func TestListAllFollowsPages(t *testing.T) {
	t.Parallel()
	const total = 230
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if size != MaxPageSize {
			t.Errorf("page_size = %d", size)
		}
		pages := (total + size - 1) / size
		var results []Project
		for i := (page - 1) * size; i < min(page*size, total); i++ {
			results = append(results, Project{ID: strconv.Itoa(i)})
		}
		var next *string
		if page < pages {
			link := fmt.Sprintf("http://x/?page=%d", page+1)
			next = &link
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": total, "next": next, "previous": nil, "page": page, "page_size": size, "total_pages": pages, "results": results,
		})
	})
	items, err := listAll[Project](context.Background(), c, testList, Params{"organization_id": "o"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != total || items[total-1].ID != strconv.Itoa(total-1) {
		t.Fatalf("got %d items", len(items))
	}
}

func TestListAllAcceptsABareArray(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(` [{"id":"a"},{"id":"b"}]`))
	})
	items, err := listAll[Project](context.Background(), c, testList, Params{"organization_id": "o"}, nil)
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %v, err = %v", items, err)
	}
}

func TestLogsNeverCarryTheAPIKeyOrPasswords(t *testing.T) {
	t.Parallel()
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"x","echo":"` + testKey + `"}`))
	})
	var output bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &output)
	body := map[string]string{"root_password": "hunter2", "note": "key " + testKey}
	if err := c.do(ctx, testPost, Params{"organization_id": "o", "project_id": "p"}, body, nil); err != nil {
		t.Fatal(err)
	}
	logs := output.String()
	if !strings.Contains(logs, "API request body") || !strings.Contains(logs, "API response") {
		t.Fatalf("expected request and response logs, got:\n%s", logs)
	}
	for _, secret := range []string{testKey, "hunter2"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs contain %q:\n%s", secret, logs)
		}
	}
}
