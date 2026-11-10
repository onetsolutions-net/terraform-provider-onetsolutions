package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	apiKeyPattern = regexp.MustCompile(`sk-onetsolutions-[A-Za-z0-9_-]+`)
	// secretJSONPattern masks the value of any JSON member whose name says it is a secret,
	// so request bodies logged at TRACE never carry a password.
	secretJSONPattern = regexp.MustCompile(`"[A-Za-z_]*(?:password|secret|token|api_key)[A-Za-z_]*"\s*:\s*"(?:[^"\\]|\\.)*"`)
	maskedFieldKeys   = []string{"authorization", "api_key", "password", "secret", "token"}
)

// MaskedContext returns ctx with the provider's log masks: the Authorization header, the
// api_key field and anything shaped like an API key never reach a log line.
func MaskedContext(ctx context.Context) context.Context {
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, maskedFieldKeys...)
	ctx = tflog.MaskAllFieldValuesRegexes(ctx, apiKeyPattern, secretJSONPattern)
	ctx = tflog.MaskMessageRegexes(ctx, apiKeyPattern, secretJSONPattern)
	return ctx
}

type loggingTransport struct {
	base http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := MaskedContext(req.Context())
	fields := map[string]any{"method": req.Method, "path": req.URL.Path}
	if req.URL.RawQuery != "" {
		fields["query"] = req.URL.RawQuery
	}
	tflog.Debug(ctx, "API request", fields)
	if req.GetBody != nil {
		if body, err := req.GetBody(); err == nil {
			content, readErr := io.ReadAll(body)
			_ = body.Close()
			if readErr == nil && len(content) > 0 {
				tflog.Trace(ctx, "API request body", map[string]any{"body": string(content)})
			}
		}
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	fields["duration_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		fields["error"] = err.Error()
		tflog.Debug(ctx, "API request failed", fields)
		return nil, err
	}
	fields["status"] = resp.StatusCode
	tflog.Debug(ctx, "API response", fields)

	if resp.Body != nil {
		content, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(content))
		if readErr != nil {
			return nil, readErr
		}
		if len(content) > 0 {
			tflog.Trace(ctx, "API response body", map[string]any{"body": string(content)})
		}
	}
	return resp, nil
}

// jitter returns a duration between d/2 and d.
func jitter(d time.Duration) time.Duration {
	half := int64(d / 2)
	if half <= 0 {
		return d
	}
	n, err := rand.Int(rand.Reader, big.NewInt(half))
	if err != nil {
		return d
	}
	return time.Duration(half + n.Int64())
}
