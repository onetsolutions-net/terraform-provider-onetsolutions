package dns

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

func configuredDomain(t *testing.T, f *fakeapi.Server) (*domainResource, *client.Client) {
	t.Helper()
	c, err := client.New(client.Config{BaseURL: f.URL(), APIKey: f.APIKey(), RequestsPerSecond: 100, RetryMax: -1})
	if err != nil {
		t.Fatal(err)
	}
	r := &domainResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &common.ProviderData{Client: c}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return r, c
}

func expectDiag(t *testing.T, diags diag.Diagnostics, summary string, details ...string) {
	t.Helper()
	for _, d := range diags {
		if d.Summary() != summary {
			continue
		}
		for _, detail := range details {
			if !strings.Contains(d.Detail(), detail) {
				t.Errorf("%q lacks %q: %s", summary, detail, d.Detail())
			}
		}
		return
	}
	t.Fatalf("no diagnostic %q among %v", summary, diags)
}

func TestDomainPlanOrderWarnsWithThePrice(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	r, _ := configuredDomain(t, f)
	scope := client.Scope{OrganizationID: f.Scope().OrganizationID, ProjectID: f.Scope().ProjectID}

	var diags diag.Diagnostics
	r.planOrder(context.Background(), scope, "brand-new.fr", &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	expectDiag(t, diags, "This plan registers a billed domain", "brand-new.fr", "for one year", "7.99 EUR the first year",
		"cannot be cancelled or refunded")

	f.Inject(http.MethodGet, "/v1/domains/availability/check", fakeapi.Injection{Status: http.StatusServiceUnavailable, Times: 10})
	diags = nil
	r.planOrder(context.Background(), scope, "brand-new.fr", &diags)
	if diags.HasError() {
		t.Fatalf("an availability outage blocked the plan: %v", diags)
	}
	expectDiag(t, diags, "Could not check the domain", "checks availability again")
}

func TestDomainOrderErrors(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	_, c := configuredDomain(t, f)
	scope := client.Scope{OrganizationID: f.Scope().OrganizationID, ProjectID: f.Scope().ProjectID}
	order := "/v1/organizations/{organization_id}/projects/{project_id}/domains/order"

	for name, tc := range map[string]struct {
		injection fakeapi.Injection
		summary   string
		detail    string
	}{
		"awaiting payment": {
			fakeapi.Injection{Status: http.StatusConflict, Body: fakeapi.Object{"error": "This domain is already on an order awaiting payment.", "domain_name": "x.fr", "invoice_id": "1"}},
			"The domain is already ordered", "Pay its invoice",
		},
		"taken meanwhile": {
			fakeapi.Injection{Status: http.StatusConflict, Body: fakeapi.Object{"error": "Domain not available", "domain_name": "x.fr", "suggestions": []any{}}},
			"Domain not available", "Nothing was ordered",
		},
		"refused": {
			fakeapi.Injection{Status: http.StatusBadRequest, Body: fakeapi.Object{"error": "Failed to create domain order. Please try again later."}},
			"Could not order the domain", "Failed to create domain order",
		},
	} {
		f.Inject(http.MethodPost, order, tc.injection)
		_, err := c.OrderDomainRegistration(context.Background(), scope, "x.fr")
		if err == nil {
			t.Fatalf("%s: the order succeeded", name)
		}
		var diags diag.Diagnostics
		orderError(err, "x.fr", &diags)
		expectDiag(t, diags, tc.summary, tc.detail)
	}
}

func TestDomainDestroyWarns(t *testing.T) {
	t.Parallel()
	r, ok := NewDomainResource().(resource.ResourceWithModifyPlan)
	if !ok {
		t.Fatal("the domain resource has no ModifyPlan")
	}
	state := map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "d"), "name": tftypes.NewValue(tftypes.String, "keep.fr"),
		"expiration_date":     tftypes.NewValue(tftypes.String, "2027-05-01"),
		"deletion_protection": tftypes.NewValue(tftypes.Bool, false),
	}
	resp := plantest.ModifyPlan(t, r, state, nil)
	expectDiag(t, resp.Diagnostics, "The domain stays registered", "keep.fr", "until it expires on 2027-05-01", "auto_renew = false")

	state["deletion_protection"] = tftypes.NewValue(tftypes.Bool, true)
	if protected := plantest.ModifyPlan(t, r, state, nil); protected.Diagnostics.WarningsCount() != 0 {
		t.Errorf("a protected destroy warned: %v", protected.Diagnostics)
	}

	failed := map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, "d"), "name": tftypes.NewValue(tftypes.String, "keep.fr"),
		"status": tftypes.NewValue(tftypes.String, "failed"), "deletion_protection": tftypes.NewValue(tftypes.Bool, false),
	}
	resp = plantest.ModifyPlan(t, r, failed, failed)
	expectDiag(t, resp.Diagnostics, "The registration of the domain failed", "-replace")
}
