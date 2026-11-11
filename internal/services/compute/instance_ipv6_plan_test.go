package compute

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
)

// Disabling IPv6 is refused at plan time; enabling it plans the address unknown.
func TestPlanIPv6(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	id := "11111111-1111-4111-8111-111111111111"
	enabled := stateOf(f, id, map[string]tftypes.Value{
		"ipv6_enabled": tftypes.NewValue(tftypes.Bool, true),
		"primary_ipv6": tftypes.NewValue(tftypes.String, "2001:db8:1::1"),
	})
	disabled := stateOf(f, id, map[string]tftypes.Value{"ipv6_enabled": tftypes.NewValue(tftypes.Bool, false)})

	resp := plantest.ModifyPlan(t, configured(t, f), enabled, disabled)
	if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Summary() != "IPv6 cannot be disabled" {
		t.Fatalf("diagnostics = %v, want the refusal to disable IPv6", resp.Diagnostics)
	}

	resp = plantest.ModifyPlan(t, configured(t, f), disabled, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, id), "organization_id": enabled["organization_id"],
		"project_id": enabled["project_id"], "name": enabled["name"],
		"ipv6_enabled": tftypes.NewValue(tftypes.Bool, true),
	})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	value, err := resp.Plan.Raw.ApplyTerraform5AttributePathStep(tftypes.AttributeName("primary_ipv6"))
	if err != nil {
		t.Fatal(err)
	}
	if planned, ok := value.(tftypes.Value); !ok || planned.IsKnown() {
		t.Fatalf("primary_ipv6 planned %v, want unknown while IPv6 is being enabled", value)
	}
}
