package common

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

// AddClientError adds an error diagnostic for err, an error of the client or of a waiter. An API
// error shows the API's message and field errors verbatim (the API never leaks internals); a
// timeout says what was awaited and that the object is still tracked.
func AddClientError(diags *diag.Diagnostics, summary string, err error) {
	diags.AddError(summary, ErrorDetail(err))
}

// AddClientErrorWithFields is AddClientError that also attaches each API field error to the
// attribute of the same name when attributes maps it (API field name → attribute path).
func AddClientErrorWithFields(diags *diag.Diagnostics, summary string, err error, attributes map[string]path.Path) {
	apiErr, ok := client.AsAPIError(err)
	if !ok || len(apiErr.Fields) == 0 {
		AddClientError(diags, summary, err)
		return
	}
	unmapped := false
	for field, messages := range apiErr.Fields {
		p, mapped := attributes[field]
		if !mapped {
			unmapped = true
			continue
		}
		diags.AddAttributeError(p, summary, strings.Join(messages, " "))
	}
	if unmapped || apiErr.Detail != "" {
		AddClientError(diags, summary, err)
	}
}

// ErrorDetail renders err for a diagnostic.
func ErrorDetail(err error) string {
	if apiErr, ok := client.AsAPIError(err); ok {
		var b strings.Builder
		if apiErr.Detail != "" {
			b.WriteString(apiErr.Detail)
		} else {
			fmt.Fprintf(&b, "The API answered HTTP %d.", apiErr.StatusCode)
		}
		for _, line := range apiErr.FieldMessages() {
			b.WriteString("\n  - ")
			b.WriteString(line)
		}
		fmt.Fprintf(&b, "\n\n(%s %s, HTTP %d", apiErr.Method, apiErr.Path, apiErr.StatusCode)
		if apiErr.Code != "" {
			fmt.Fprintf(&b, ", code %s", apiErr.Code)
		}
		b.WriteString(")")
		return b.String()
	}
	var timeout *waiter.TimeoutError
	if errors.As(err, &timeout) {
		return timeout.Error() + ". The object exists and stays in the state. If it was being created, " +
			"Terraform marks it tainted and the next apply replaces it: it destroys the object and creates " +
			"it again, which for a billed object places a new order. To keep it instead, wait until it is " +
			"ready and run `terraform untaint` on its address before applying again."
	}
	return err.Error()
}
