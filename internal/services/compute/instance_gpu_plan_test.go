package compute

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

func configuredGPU(t *testing.T, f *fakeapi.Server) *instanceGPUResource {
	t.Helper()
	c, err := client.New(client.Config{BaseURL: f.URL(), APIKey: f.APIKey(), RequestsPerSecond: 100, RetryMax: -1})
	if err != nil {
		t.Fatal(err)
	}
	r := &instanceGPUResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &common.ProviderData{Client: c}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return r
}

func gpuPlan(f *fakeapi.Server, instanceID tftypes.Value, model string) map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"organization_id":     tftypes.NewValue(tftypes.String, f.Scope().OrganizationID),
		"project_id":          tftypes.NewValue(tftypes.String, f.Scope().ProjectID),
		"instance_id":         instanceID,
		"model":               tftypes.NewValue(tftypes.String, model),
		"deletion_protection": tftypes.NewValue(tftypes.Bool, false),
	}
}

func TestGPUPlanWarnsWithThePrice(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	instanceID := f.SeedInstance(f.Scope(), nil).ID()
	f.SeedGPU(f.Scope(), instanceID, fakeapi.Object{"model": "NVIDIA A100", "hourly_rate": "2.1000"})
	known := tftypes.NewValue(tftypes.String, instanceID)

	resp := plantest.ModifyPlan(t, configuredGPU(t, f), nil, gpuPlan(f, known, "NVIDIA A100"))
	expectWarning(t, resp.Diagnostics, "This plan attaches a billed GPU", "NVIDIA A100", instanceID,
		"billed 2.1000 per hour", "stopped and started again")

	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	resp = plantest.ModifyPlan(t, configuredGPU(t, f), nil, gpuPlan(f, unknown, "NVIDIA A100"))
	expectWarning(t, resp.Diagnostics, "This plan attaches a billed GPU", "billed by the hour")

	f.Inject(http.MethodGet, "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{id}/gpus/available",
		fakeapi.Injection{Status: http.StatusForbidden})
	resp = plantest.ModifyPlan(t, configuredGPU(t, f), nil, gpuPlan(f, known, "NVIDIA A100"))
	expectWarning(t, resp.Diagnostics, "This plan attaches a billed GPU", "could not be listed")
}

// A destroy plan says the GPU is detached and the instance restarted; a protected attachment gets
// no warning (its destroy fails).
func TestGPUPlanWarnsOnDetach(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	state := gpuPlan(f, tftypes.NewValue(tftypes.String, "11111111-1111-4111-8111-111111111111"), "NVIDIA L4")
	state["id"] = tftypes.NewValue(tftypes.String, "22222222-2222-4222-8222-222222222222")

	resp := plantest.ModifyPlan(t, configuredGPU(t, f), state, nil)
	expectWarning(t, resp.Diagnostics, "This plan detaches a GPU", "ends its billing", "stopped and started again")

	state["deletion_protection"] = tftypes.NewValue(tftypes.Bool, true)
	resp = plantest.ModifyPlan(t, configuredGPU(t, f), state, nil)
	if len(resp.Diagnostics.Warnings()) != 0 {
		t.Fatalf("warnings = %v for a protected attachment", warningsOf(resp.Diagnostics))
	}
}

func TestGPUPlanWarnsOnReplacement(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	instanceID := tftypes.NewValue(tftypes.String, "11111111-1111-4111-8111-111111111111")
	state := gpuPlan(f, instanceID, "NVIDIA L4")
	state["id"] = tftypes.NewValue(tftypes.String, "22222222-2222-4222-8222-222222222222")
	plan := gpuPlan(f, instanceID, "NVIDIA A100")

	resp := plantest.ModifyPlan(t, configuredGPU(t, f), state, plan)
	expectWarning(t, resp.Diagnostics, "Replacing this GPU restarts the instance twice", "NVIDIA L4", `"NVIDIA A100"`)
}
