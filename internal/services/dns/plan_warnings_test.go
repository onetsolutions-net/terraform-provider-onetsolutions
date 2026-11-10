package dns

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/plantest"
)

func hasWarning(diags diag.Diagnostics, summary, detail string) bool {
	for _, w := range diags.Warnings() {
		if w.Summary() == summary && w.Detail() == detail {
			return true
		}
	}
	return false
}

func TestDNSSECDisableWarns(t *testing.T) {
	t.Parallel()
	r, ok := NewDNSSECResource().(resource.ResourceWithModifyPlan)
	if !ok {
		t.Fatal("the DNSSEC resource has no ModifyPlan")
	}
	enabled := map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "z"), "enabled": tftypes.NewValue(tftypes.Bool, true)}
	disabled := map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "z"), "enabled": tftypes.NewValue(tftypes.Bool, false)}

	for name, tc := range map[string]struct {
		state, plan map[string]tftypes.Value
		warn        bool
	}{
		"create":           {nil, enabled, false},
		"unchanged":        {enabled, enabled, false},
		"enable":           {disabled, enabled, false},
		"disable":          {enabled, disabled, true},
		"destroy":          {enabled, nil, true},
		"destroy unsigned": {disabled, nil, false},
	} {
		resp := plantest.ModifyPlan(t, r, tc.state, tc.plan)
		if resp.Diagnostics.HasError() {
			t.Fatalf("%s: %v", name, resp.Diagnostics)
		}
		if got := hasWarning(resp.Diagnostics, "This plan disables DNSSEC", DNSSECWarning); got != tc.warn {
			t.Errorf("%s: warned = %v, want %v (%v)", name, got, tc.warn, resp.Diagnostics)
		}
	}
}

func TestDomainSettingsDestroyWarns(t *testing.T) {
	t.Parallel()
	r, ok := NewDomainSettingsResource().(resource.ResourceWithModifyPlan)
	if !ok {
		t.Fatal("the domain settings resource has no ModifyPlan")
	}
	settings := map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "d"), "auto_renew": tftypes.NewValue(tftypes.Bool, true)}

	if resp := plantest.ModifyPlan(t, r, settings, nil); !hasWarning(resp.Diagnostics, "The domain is left unchanged", DomainSettingsDeleteNote) {
		t.Errorf("destroy did not warn: %v", resp.Diagnostics)
	}
	if resp := plantest.ModifyPlan(t, r, settings, settings); resp.Diagnostics.WarningsCount() != 0 {
		t.Errorf("an update warned: %v", resp.Diagnostics)
	}
}
