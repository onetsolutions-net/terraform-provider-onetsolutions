// Package client is the typed OnetSolutions API client shared by every resource: transport,
// retries, rate limiting, pagination, error decoding, plus one file per area with that area's
// request/response structs and methods.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"
)

// DefaultBaseURL is the public API.
const DefaultBaseURL = "https://api.onetsolutions.net"

// Defaults of Config.
const (
	DefaultRequestsPerSecond = 10.0
	DefaultBurst             = 20
	DefaultRetryMax          = 5
	DefaultMinRetryWait      = time.Second
	DefaultMaxRetryWait      = 30 * time.Second
	DefaultRequestTimeout    = 2 * time.Minute
)

// Config configures a Client. Zero values take the defaults above.
type Config struct {
	BaseURL           string
	APIKey            string
	UserAgent         string
	RequestsPerSecond float64
	Burst             int
	RetryMax          int
	RetryWaitMin      time.Duration
	RetryWaitMax      time.Duration
	RequestTimeout    time.Duration
	// Transport replaces the default pooled transport, for tests.
	Transport http.RoundTripper
}

// Client calls the API. It is safe for concurrent use.
type Client struct {
	baseURL   *url.URL
	apiKey    string
	userAgent string
	http      *retryablehttp.Client
}

// New builds a Client from cfg.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	base, err := ParseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	applyDefaults(&cfg)

	transport := cfg.Transport
	if transport == nil {
		transport = cleanhttp.DefaultPooledTransport()
	}
	limiter := rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), cfg.Burst)

	retrying := retryablehttp.NewClient()
	retrying.HTTPClient = &http.Client{
		Transport: &loggingTransport{base: &rateLimitedTransport{base: transport, limiter: limiter}},
		Timeout:   cfg.RequestTimeout,
		// A redirect would resend the API key to another URL and turn a POST into a GET, so it
		// is answered as an error instead of being followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	retrying.Logger = nil
	retrying.RetryMax = cfg.RetryMax
	retrying.RetryWaitMin = cfg.RetryWaitMin
	retrying.RetryWaitMax = cfg.RetryWaitMax
	retrying.CheckRetry = checkRetry
	retrying.Backoff = backoff
	retrying.ErrorHandler = retryablehttp.PassthroughErrorHandler

	return &Client{baseURL: base, apiKey: cfg.APIKey, userAgent: cfg.UserAgent, http: retrying}, nil
}

// ParseBaseURL parses an API URL: https, or http only for a loopback host, so the API key never
// travels in cleartext over a network.
func ParseBaseURL(raw string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid API URL %q: %w", raw, err)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q: no host", raw)
	}
	switch base.Scheme {
	case "https":
		return base, nil
	case "http":
		if isLoopback(base.Hostname()) {
			return base, nil
		}
		return nil, fmt.Errorf("invalid API URL %q: http is allowed only for localhost, 127.0.0.1 or ::1; use https", raw)
	default:
		return nil, fmt.Errorf("invalid API URL %q: the scheme must be https (or http for localhost)", raw)
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func applyDefaults(cfg *Config) {
	if cfg.RequestsPerSecond <= 0 {
		cfg.RequestsPerSecond = DefaultRequestsPerSecond
	}
	if cfg.Burst <= 0 {
		cfg.Burst = DefaultBurst
	}
	if cfg.RetryMax < 0 {
		cfg.RetryMax = 0
	} else if cfg.RetryMax == 0 {
		cfg.RetryMax = DefaultRetryMax
	}
	if cfg.RetryWaitMin <= 0 {
		cfg.RetryWaitMin = DefaultMinRetryWait
	}
	if cfg.RetryWaitMax <= 0 {
		cfg.RetryWaitMax = DefaultMaxRetryWait
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
}

// UserAgent builds the User-Agent header the provider sends.
func UserAgent(providerVersion, terraformVersion string) string {
	if providerVersion == "" {
		providerVersion = "dev"
	}
	if terraformVersion == "" {
		terraformVersion = "unknown"
	}
	return fmt.Sprintf(
		"terraform-provider-onetsolutions/%s (terraform/%s; +https://registry.terraform.io/providers/onetsolutions-net/onetsolutions)",
		providerVersion, terraformVersion,
	)
}

// do sends one request on route r and decodes a 2xx JSON answer into out (when non-nil).
func (c *Client) do(ctx context.Context, r Route, params Params, body, out any) error {
	raw, err := c.doRaw(ctx, r, params, nil, body)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: decoding the response: %w", r.method, r.template, err)
	}
	return nil
}

func (c *Client) doRaw(ctx context.Context, r Route, params Params, query url.Values, body any) ([]byte, error) {
	path, err := r.expand(params)
	if err != nil {
		return nil, err
	}
	target := *c.baseURL
	// expand path-escapes every parameter: the escaped form goes to RawPath, so String() sends it
	// as is instead of escaping its "%" a second time.
	target.RawPath = c.baseURL.EscapedPath() + path
	if target.Path, err = url.PathUnescape(target.RawPath); err != nil {
		return nil, fmt.Errorf("%s %s: %w", r.method, r.template, err)
	}
	if len(query) > 0 {
		target.RawQuery = query.Encode()
	}

	var payload any
	if body != nil {
		encoded, encodeErr := json.Marshal(body)
		if encodeErr != nil {
			return nil, fmt.Errorf("%s %s: encoding the request: %w", r.method, r.template, encodeErr)
		}
		payload = encoded
	}

	ctx = context.WithValue(ctx, retrySafeKey{}, r.retrySafe)
	req, err := retryablehttp.NewRequestWithContext(ctx, r.method, target.String(), payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s %s: %w", r.method, path, ctxErr)
		}
		return nil, fmt.Errorf("%s %s: %w", r.method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: reading the response: %w", r.method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseAPIError(resp.StatusCode, r.method, path, raw)
	}
	return raw, nil
}

type retrySafeKey struct{}

// checkRetry retries a 429 whatever the method (the API refused the request before handling
// it), and transport errors and 502/503/504 only for retry-safe routes: a create is never sent
// twice because its first answer was lost.
func checkRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	safe, _ := ctx.Value(retrySafeKey{}).(bool)
	if err != nil {
		if !safe {
			return false, nil
		}
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true, nil
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return safe, nil
	default:
		return false, nil
	}
}

// backoff honors Retry-After on 429/503 and otherwise waits an exponentially growing,
// jittered delay between min and max.
func backoff(minWait, maxWait time.Duration, attempt int, resp *http.Response) time.Duration {
	if resp != nil && resp.Header.Get("Retry-After") != "" &&
		(resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) {
		return retryablehttp.DefaultBackoff(minWait, maxWait, attempt, resp)
	}
	wait := minWait
	for i := 0; i < attempt && wait < maxWait; i++ {
		wait *= 2
	}
	if wait > maxWait {
		wait = maxWait
	}
	return jitter(wait)
}

type rateLimitedTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

// IsCanceled reports whether err comes from a canceled or expired context.
func IsCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
