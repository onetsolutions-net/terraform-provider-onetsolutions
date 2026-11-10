package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

// The create-only inputs warn on change and never replace the instance (plan section 5.2);
// common's tests cover the modifier's behavior, this one that every such input uses it.
func TestInstanceSchema_createOnlyInputsWarnWithoutReplacing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	NewInstanceResource().Schema(ctx, resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}

	modifiersOf := func(name string) []any {
		if block, ok := resp.Schema.Blocks[name].(schema.ListNestedBlock); ok {
			return toAny(block.PlanModifiers)
		}
		switch a := resp.Schema.Attributes[name].(type) {
		case schema.SetAttribute:
			return toAny(a.PlanModifiers)
		case schema.StringAttribute:
			return toAny(a.PlanModifiers)
		case schema.BoolAttribute:
			return toAny(a.PlanModifiers)
		default:
			t.Fatalf("%s: unexpected attribute type %T", name, a)
			return nil
		}
	}
	for _, name := range []string{
		"ssh_key_ids", "volume_id", "control_panel", "one_click_app", "addon_ids", "accepted_contract_ids",
		"install_management_key", "allow_password_reset",
	} {
		found := false
		for _, modifier := range modifiersOf(name) {
			createOnly, ok := modifier.(common.CreateOnlyModifier)
			if !ok {
				continue
			}
			found = true
			if !strings.Contains(createOnly.Description(ctx), "stored without affecting") {
				t.Errorf("%s: the create-only modifier replaces instead of warning: %s", name, createOnly.Description(ctx))
			}
		}
		if !found {
			t.Errorf("%s has no create-only modifier", name)
		}
	}
}

func toAny[T any](items []T) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}
