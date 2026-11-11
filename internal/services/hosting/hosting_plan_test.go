package hosting

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

const (
	individualPlan = "c2e74304-62c9-4bbe-9b9e-aaf3bddef705"
	businessPlan   = "a3db8965-dc94-485f-b0dc-c6f2d1d87e24"
)

func configuredClient(t *testing.T, f *fakeapi.Server) *common.ProviderData {
	t.Helper()
	c, err := client.New(client.Config{BaseURL: f.URL(), APIKey: f.APIKey(), RequestsPerSecond: 100, RetryMax: -1})
	if err != nil {
		t.Fatal(err)
	}
	return &common.ProviderData{Client: c}
}

func configuredWebHosting(t *testing.T, f *fakeapi.Server) *webHostingResource {
	t.Helper()
	r := &webHostingResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: configuredClient(t, f)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return r
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
	t.Fatalf("no warning %q among %v", summary, diags.Warnings())
}

func TestPreviewOrderWarnsWithThePrice(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	r := configuredWebHosting(t, f)
	plan := webHostingModel{
		Domain: types.StringValue("example.fr"), PlanID: types.StringValue(individualPlan),
		BillingCycle: types.StringValue("annually"), DomainSource: types.StringValue(sourceRegister),
	}
	var diags diag.Diagnostics
	r.previewOrder(context.Background(), &plan, &diags)
	expectWarning(t, diags, "This plan orders a billed web hosting",
		"example.fr", mustPlan(t, f, individualPlan).String("name"), "28.70 EUR", "annually", "registers the domain")
}

func TestPreviewOrderDefaultsToMonthly(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	r := configuredWebHosting(t, f)
	plan := webHostingModel{Domain: types.StringUnknown(), PlanID: types.StringValue(businessPlan), BillingCycle: types.StringUnknown()}
	var diags diag.Diagnostics
	r.previewOrder(context.Background(), &plan, &diags)
	expectWarning(t, diags, "This plan orders a billed web hosting", "the domain", "9.99 EUR", "monthly")
}

func TestPlanChangeWarnsWithTheNewPrice(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	hosting := f.SeedWebHosting(f.Scope(), fakeapi.Object{"plan": mustPlan(t, f, individualPlan)})
	r := configuredWebHosting(t, f)
	scope := f.Scope()
	state := webHostingModel{
		ID: types.StringValue(hosting.ID()), OrganizationID: types.StringValue(scope.OrganizationID),
		ProjectID: types.StringValue(scope.ProjectID), PlanID: types.StringValue(individualPlan),
		BillingCycle: types.StringValue("monthly"),
	}
	plan := state
	plan.PlanID = types.StringValue(businessPlan)
	plan.BillingCycle = types.StringValue("biennially")
	var diags diag.Diagnostics
	r.planChange(context.Background(), &plan, &state, &diags)
	expectWarning(t, diags, "This plan changes the billing of a web hosting", "Hébergement Business", "167.83 EUR", "biennially")
}

func mustPlan(t *testing.T, f *fakeapi.Server, id string) fakeapi.Object {
	t.Helper()
	plan, ok := f.Get(fakeapi.KindHostingPlan, id)
	if !ok {
		t.Fatalf("no plan %s", id)
	}
	return plan
}

func TestDestroyWarnsOfTheCancellation(t *testing.T) {
	t.Parallel()
	r := &webHostingResource{}
	str := tftypes.NewValue(tftypes.String, "x")
	state := map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, "8b1d4e2f-6c3a-4f9e-8d7b-2a1c0e9f8d7c"),
		"domain":              tftypes.NewValue(tftypes.String, "example.fr"),
		"next_billing_date":   tftypes.NewValue(tftypes.String, "2026-12-01T00:00:00Z"),
		"deletion_protection": tftypes.NewValue(tftypes.Bool, false),
		"organization_id":     str, "project_id": str,
	}
	resp := plantest.ModifyPlan(t, r, state, nil)
	expectWarning(t, resp.Diagnostics, "This plan cancels a billed web hosting",
		"example.fr", "cancellation at the end of the billing period", "2026-12-01T00:00:00Z")

	state["deletion_protection"] = tftypes.NewValue(tftypes.Bool, true)
	if resp := plantest.ModifyPlan(t, r, state, nil); len(resp.Diagnostics) != 0 {
		t.Fatalf("a protected web hosting warns: %v", resp.Diagnostics)
	}
}

func TestAwaitingPaymentDetail(t *testing.T) {
	t.Parallel()
	number, url, invoice, due := "7001", "https://console.onetsolutions.net/billing/invoices/1", int64(1), 3.59
	detail := awaitingPaymentDetail(&client.HostingOrder{
		OrderNumber: &number, InvoiceID: &invoice, InvoicePaymentURL: &url, AmountDue: &due, DomainName: "example.fr",
	})
	for _, want := range []string{"Order 7001", "invoice 1", "example.fr", "3.59", url, "pending"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail lacks %q: %s", want, detail)
		}
	}
}

func TestCycleRank(t *testing.T) {
	t.Parallel()
	if webHostingCycleRank("monthly") >= webHostingCycleRank("annually") || webHostingCycleRank("free") != 0 {
		t.Fatal("billing cycles are not ordered from the shortest")
	}
}
