package compute

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

// configured returns an instance resource whose client talks to f.
func configured(t *testing.T, f *fakeapi.Server) *instanceResource {
	t.Helper()
	c, err := client.New(client.Config{BaseURL: f.URL(), APIKey: f.APIKey(), RequestsPerSecond: 100, RetryMax: -1})
	if err != nil {
		t.Fatal(err)
	}
	r := &instanceResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &common.ProviderData{Client: c}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return r
}

func sizeBySlug(t *testing.T, f *fakeapi.Server, slug string) string {
	t.Helper()
	for _, size := range f.All(fakeapi.KindSize) {
		if size.String("slug") == slug {
			return size.ID()
		}
	}
	t.Fatalf("no size %s in the catalog fixture", slug)
	return ""
}

func warningsOf(diags diag.Diagnostics) []string {
	var out []string
	for _, w := range diags.Warnings() {
		out = append(out, w.Summary()+": "+w.Detail())
	}
	return out
}

func expectWarning(t *testing.T, diags diag.Diagnostics, summary string, details ...string) {
	t.Helper()
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags.Errors())
	}
	for _, w := range diags.Warnings() {
		if w.Summary() != summary {
			continue
		}
		for _, detail := range details {
			if !strings.Contains(w.Detail(), detail) {
				t.Errorf("warning %q lacks %q: %s", summary, detail, w.Detail())
			}
		}
		return
	}
	t.Fatalf("no warning %q among %v", summary, warningsOf(diags))
}

func TestPreviewOrderWarnsWithThePrice(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	scope := f.Scope()
	plan := instanceModel{
		OrganizationID: types.StringValue(scope.OrganizationID), ProjectID: types.StringValue(scope.ProjectID),
		Name: types.StringValue("web-1"), SizeID: types.StringValue(sizeBySlug(t, f, "p-4")),
		ImageID: types.StringValue(f.First(fakeapi.KindPublicImage).ID()), BillingCycle: types.StringValue("monthly"),
	}
	var diags diag.Diagnostics
	configured(t, f).previewOrder(context.Background(), &plan, &diags)
	expectWarning(t, diags, "This plan orders a billed instance", `"web-1"`, "7.99 EUR", "monthly billing cycle")

	f.Inject(http.MethodPost, "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/order-preview",
		fakeapi.Injection{Status: http.StatusServiceUnavailable, Times: 10})
	diags = nil
	configured(t, f).previewOrder(context.Background(), &plan, &diags)
	expectWarning(t, diags, "Could not price the instance order", "orders a billed instance")
}

func TestPlanResizeWarnsWithThePrice(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	scope := f.Scope()
	base, larger := sizeBySlug(t, f, "p-4"), sizeBySlug(t, f, "p-8")
	seeded := f.SeedInstance(scope, fakeapi.Object{"size": mustFind(t, f, base)})
	state := instanceModel{
		ID: types.StringValue(seeded.ID()), OrganizationID: types.StringValue(scope.OrganizationID),
		ProjectID: types.StringValue(scope.ProjectID), SizeID: types.StringValue(base),
		BillingCycle: types.StringValue("monthly"), State: types.StringValue(stateStopped),
	}
	plan := state
	plan.SizeID = types.StringValue(larger)
	plan.BillingCycle = types.StringValue("annually")
	plan.AllowStoppingForUpdate = types.BoolValue(false)

	var diags diag.Diagnostics
	configured(t, f).planResize(context.Background(), &plan, &state, &diags)
	expectWarning(t, diags, "This plan changes the billing of an instance", seeded.ID(), "annually billing cycle", "once the order is paid")
}

func TestCheckResizableRefusesACycleAlone(t *testing.T) {
	t.Parallel()
	state := instanceModel{SizeID: types.StringValue("s"), BillingCycle: types.StringValue("monthly")}
	for name, tc := range map[string]struct {
		size, cycle string
		ok          bool
	}{
		"unchanged":      {"s", "monthly", true},
		"size only":      {"t", "monthly", true},
		"size and cycle": {"t", "annually", true},
		"cycle alone":    {"s", "annually", false},
	} {
		plan := instanceModel{SizeID: types.StringValue(tc.size), BillingCycle: types.StringValue(tc.cycle)}
		var diags diag.Diagnostics
		if checkResizable(&plan, &state, &diags) != tc.ok || diags.HasError() == tc.ok {
			t.Errorf("%s: ok = %v, diagnostics = %v", name, !diags.HasError(), diags)
		}
	}
}

func mustFind(t *testing.T, f *fakeapi.Server, sizeID string) fakeapi.Object {
	t.Helper()
	size, ok := f.Get(fakeapi.KindSize, sizeID)
	if !ok {
		t.Fatalf("size %s not found", sizeID)
	}
	return size
}

func orderedFrom(id string, localOrderID *string) client.Instance {
	return client.Instance{ID: id, Name: "web", OrderID: client.OptionalField{Present: true, Value: localOrderID}}
}

func TestOrderedInstance(t *testing.T) {
	t.Parallel()
	mine, other := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"

	found, err := orderedInstance([]client.Instance{orderedFrom("a", &other), orderedFrom("b", &mine), orderedFrom("c", nil)}, mine)
	if err != nil || found == nil || found.ID != "b" {
		t.Fatalf("found %v, %v; want the instance whose order_id is the order", found, err)
	}
	if found, err := orderedInstance(nil, mine); found != nil || err != nil {
		t.Fatalf("found %v, %v before the instance is listed; want to keep waiting", found, err)
	}
	if found, err := orderedInstance([]client.Instance{orderedFrom("a", &other)}, mine); found != nil || err != nil {
		t.Fatalf("found %v, %v among instances of other orders; want to keep waiting", found, err)
	}
	// An API that ignores the filter and serves no order_id: the same-name instance is never adopted.
	legacy := []client.Instance{{ID: "a", Name: "web"}, {ID: "b", Name: "web"}}
	if found, err := orderedInstance(legacy, mine); found != nil || !errors.Is(err, errAPITooOld) {
		t.Fatalf("found %v, %v without order_id; want errAPITooOld", found, err)
	}
	if _, err := orderedInstance([]client.Instance{orderedFrom("b", &mine), orderedFrom("a", &mine)}, mine); err == nil ||
		!strings.Contains(err.Error(), "(a, b)") {
		t.Fatalf("err = %v, want both instances of the order named", err)
	}
}

func TestOptionalFieldTellsAbsentFromNull(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]client.OptionalField{
		`{"id": "a"}`:                      {},
		`{"id": "a", "order_id": null}`:    {Present: true},
		`{"id": "a", "order_id": "x-y-z"}`: {Present: true, Value: ptr("x-y-z")},
	} {
		var instance client.Instance
		if err := json.Unmarshal([]byte(body), &instance); err != nil {
			t.Fatal(err)
		}
		got := instance.OrderID
		if got.Present != want.Present || (got.Value == nil) != (want.Value == nil) || got.Value != nil && *got.Value != *want.Value {
			t.Errorf("%s: OrderID = %+v, want %+v", body, got, want)
		}
	}
}

func ptr(s string) *string { return &s }

func TestCountWords(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]int{
		"":                      0,
		"Too expensive for us.": 4,
		"We no longer need it, l'équipe a migré.": 9,
		"snake_case counts as two":                5,
		"v2 -- 3.5 ok":                            4,
	} {
		if got := countWords(text); got != want {
			t.Errorf("countWords(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestCancellationDetail(t *testing.T) {
	t.Parallel()
	end := "2026-12-01T00:00:00Z"
	requested := cancellationDetail(&client.Instance{ID: "i-1", Name: "web", Status: "active", BillingEndDate: &end})
	for _, part := range []string{"was not deleted", "keeps running and billing until " + end, "revoke the cancellation"} {
		if !strings.Contains(requested, part) {
			t.Errorf("detail %q lacks %q", requested, part)
		}
	}
	if undated := cancellationDetail(&client.Instance{ID: "i-1", Status: "active"}); !strings.Contains(undated, "until the end of its billing period") {
		t.Errorf("detail without billing_end_date: %q", undated)
	}
	if ended := cancellationDetail(&client.Instance{ID: "i-1", Status: "terminated"}); !strings.Contains(ended, "already ended") {
		t.Errorf("detail of an ended instance: %q", ended)
	}
}

func TestAwaitingPaymentDetailNamesTheOrder(t *testing.T) {
	t.Parallel()
	local, invoice, due := "33333333-3333-4333-8333-333333333333", int64(42), 9.6
	detail := awaitingPaymentDetail(&client.InstanceOrder{OrderNumber: "9000000", LocalOrderID: &local, InvoiceID: &invoice, AmountDue: &due}, "web")
	for _, part := range []string{"local order id " + local, "invoice 42", "9.60 is due", "terraform import <address> order:" + local} {
		if !strings.Contains(detail, part) {
			t.Errorf("detail %q lacks %q", detail, part)
		}
	}
}

func TestOrderRefused(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err     error
		refused bool
	}{
		"validation":      {&client.APIError{StatusCode: http.StatusBadRequest}, true},
		"throttled":       {&client.APIError{StatusCode: http.StatusTooManyRequests}, true},
		"redirect":        {&client.APIError{StatusCode: http.StatusMovedPermanently}, true},
		"request timeout": {&client.APIError{StatusCode: http.StatusRequestTimeout}, false},
		"server error":    {&client.APIError{StatusCode: http.StatusInternalServerError}, false},
		"gateway timeout": {&client.APIError{StatusCode: http.StatusGatewayTimeout}, false},
		"transport":       {errors.New("connection reset by peer"), false},
	} {
		if got := orderRefused(tc.err); got != tc.refused {
			t.Errorf("%s: orderRefused = %v, want %v", name, got, tc.refused)
		}
	}
}
