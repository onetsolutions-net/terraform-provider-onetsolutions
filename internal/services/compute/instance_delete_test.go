package compute

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

const instanceDeleteRoute = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{id}"

// deleteInstance runs the resource's Delete on a state holding attrs.
func deleteInstance(t *testing.T, f *fakeapi.Server, attrs map[string]tftypes.Value) *resource.DeleteResponse {
	t.Helper()
	r := configured(t, f)
	value, schemaResp := plantest.Value(t, r, attrs)
	state := tfsdk.State{Schema: schemaResp.Schema, Raw: value}
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	return resp
}

func stateOf(f *fakeapi.Server, id string, extra map[string]tftypes.Value) map[string]tftypes.Value {
	attrs := map[string]tftypes.Value{
		"id":              tftypes.NewValue(tftypes.String, id),
		"organization_id": tftypes.NewValue(tftypes.String, f.Scope().OrganizationID),
		"project_id":      tftypes.NewValue(tftypes.String, f.Scope().ProjectID),
		"name":            tftypes.NewValue(tftypes.String, "web-1"),
	}
	for name, value := range extra {
		attrs[name] = value
	}
	return attrs
}

// The destroy sends the reason code and reason of the state and warns that the instance keeps
// running and billing until the billing_end_date the API answers.
func TestDeleteRequestsCancellationAndWarnsWithTheEndDate(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	const nextBilling = "2026-12-01T00:00:00Z"
	seeded := f.SeedInstance(f.Scope(), fakeapi.Object{"name": "web-1", "next_billing_date": nextBilling})
	const reason = "The project ends and the team moves to another platform."

	resp := deleteInstance(t, f, stateOf(f, seeded.ID(), map[string]tftypes.Value{
		"cancellation_reason_code": tftypes.NewValue(tftypes.String, "technical_issue"),
		"cancellation_reason":      tftypes.NewValue(tftypes.String, reason),
	}))
	expectWarning(t, resp.Diagnostics, "The instance's cancellation is requested", seeded.ID(),
		"keeps running and billing until "+nextBilling, "revoke the cancellation")
	stored, _ := f.Get(fakeapi.KindInstance, seeded.ID())
	if stored.String("cancellation_reason_code") != "technical_issue" || stored.String("cancellation_reason") != reason ||
		stored.String("billing_end_date") != nextBilling {
		t.Fatalf("the API recorded %v", stored)
	}
}

// Without a reason code in the state (an older state), the default is sent; the reason is left
// out so the API's default sentence applies.
func TestDeleteSendsTheDefaultReason(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	seeded := f.SeedInstance(f.Scope(), nil)

	resp := deleteInstance(t, f, stateOf(f, seeded.ID(), nil))
	expectWarning(t, resp.Diagnostics, "The instance's cancellation is requested", "keeps running and billing until")
	stored, _ := f.Get(fakeapi.KindInstance, seeded.ID())
	if stored.String("cancellation_reason_code") != client.DefaultCancellationReasonCode || stored.String("cancellation_reason") != fakeapi.DeleteInstanceDefaultReason {
		t.Fatalf("the API recorded %q / %q", stored.String("cancellation_reason_code"), stored.String("cancellation_reason"))
	}
}

// deletion_protection refuses the destroy before any request; an instance already gone (404)
// leaves the state without an error.
func TestDeleteProtectedAndGone(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	seeded := f.SeedInstance(f.Scope(), nil)

	resp := deleteInstance(t, f, stateOf(f, seeded.ID(), map[string]tftypes.Value{
		"deletion_protection": tftypes.NewValue(tftypes.Bool, true),
	}))
	if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "The instance is protected from deletion" {
		t.Fatalf("diagnostics = %v, want the protection error", resp.Diagnostics)
	}
	if n := f.CountRequests(http.MethodDelete, instanceDeleteRoute); n != 0 {
		t.Fatalf("DELETE sent %d times for a protected instance", n)
	}

	f.Delete(fakeapi.KindInstance, seeded.ID())
	resp = deleteInstance(t, f, stateOf(f, seeded.ID(), nil))
	if resp.Diagnostics.HasError() || len(resp.Diagnostics.Warnings()) != 0 {
		t.Fatalf("diagnostics = %v for an instance already gone, want none", resp.Diagnostics)
	}
}

// A destroy plan says the instance is cancelled at the end of its period, not deleted; a
// protected instance gets no warning (its destroy fails).
func TestModifyPlanWarnsThatDestroyCancels(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	state := stateOf(f, "11111111-1111-4111-8111-111111111111", map[string]tftypes.Value{
		"next_billing_date": tftypes.NewValue(tftypes.String, "2026-12-01T00:00:00Z"),
	})
	resp := plantest.ModifyPlan(t, configured(t, f), state, nil)
	expectWarning(t, resp.Diagnostics, "This plan cancels a billed instance",
		"does not delete it", "keeps running and billing until 2026-12-01T00:00:00Z")

	state["deletion_protection"] = tftypes.NewValue(tftypes.Bool, true)
	resp = plantest.ModifyPlan(t, configured(t, f), state, nil)
	if len(resp.Diagnostics.Warnings()) != 0 {
		t.Fatalf("warnings = %v for a protected instance", warningsOf(resp.Diagnostics))
	}
}
