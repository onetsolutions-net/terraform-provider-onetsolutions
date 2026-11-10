// Package plantest builds ModifyPlan requests for unit tests of a resource's plan-time
// diagnostics (warnings the Terraform test framework does not expose).
package plantest

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Value builds an object of r's schema holding attrs, every other attribute null. A nil attrs
// is the null object: no prior state on create, no planned state on destroy.
func Value(t *testing.T, r resource.Resource, attrs map[string]tftypes.Value) (tftypes.Value, resource.SchemaResponse) {
	t.Helper()
	ctx := context.Background()
	schemaResp := resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatal(schemaResp.Diagnostics)
	}
	typ, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}
	if attrs == nil {
		return tftypes.NewValue(typ, nil), schemaResp
	}
	values := map[string]tftypes.Value{}
	for name, attrType := range typ.AttributeTypes {
		if value, set := attrs[name]; set {
			values[name] = value
			continue
		}
		values[name] = tftypes.NewValue(attrType, nil)
	}
	return tftypes.NewValue(typ, values), schemaResp
}

// ModifyPlan runs r's ModifyPlan from state to plan (config = plan) and returns its response.
func ModifyPlan(t *testing.T, r resource.ResourceWithModifyPlan, state, plan map[string]tftypes.Value) *resource.ModifyPlanResponse {
	t.Helper()
	stateValue, schemaResp := Value(t, r, state)
	planValue, _ := Value(t, r, plan)
	req := resource.ModifyPlanRequest{
		State:  tfsdk.State{Schema: schemaResp.Schema, Raw: stateValue},
		Plan:   tfsdk.Plan{Schema: schemaResp.Schema, Raw: planValue},
		Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: planValue},
	}
	resp := &resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(context.Background(), req, resp)
	return resp
}
