package common

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

// importedKey is the private state key ImportState sets on a resource with create-only inputs.
const importedKey = "imported"

// MarkImported records in the private state that the object was imported, so its create-only
// inputs are taken from the next configuration silently (see CreateOnlyModifier).
func MarkImported(ctx context.Context, private interface {
	SetKey(context.Context, string, []byte) diag.Diagnostics
}) diag.Diagnostics {
	return private.SetKey(ctx, importedKey, []byte("true"))
}

// ClearImported removes the marker of MarkImported; an apply calls it once the configured
// create-only inputs are stored.
func ClearImported(ctx context.Context, private interface {
	SetKey(context.Context, string, []byte) diag.Diagnostics
}) diag.Diagnostics {
	return private.SetKey(ctx, importedKey, nil)
}

type privateReader interface {
	GetKey(context.Context, string) ([]byte, diag.Diagnostics)
}

func imported(ctx context.Context, private privateReader) bool {
	value, diags := private.GetKey(ctx, importedKey)
	return !diags.HasError() && string(value) == "true"
}

// CreateOnlyModifier handles an input the API accepts at creation only, never returns and cannot
// update. It never acts on create, nor after an import (MarkImported) until the next apply: the
// API cannot say what was installed, so the value is taken from the configuration silently.
// Otherwise any change, including setting an input left unset at creation, is stored in state
// with a plan-time warning that it has no effect on the existing object; it never replaces it.
//
// It implements the plan modifier interface of every attribute type.
type CreateOnlyModifier struct {
	effect string
}

// CreateOnlyWarn returns a CreateOnlyModifier. effect completes the warning, e.g. "It takes
// effect on a reinstall or a new instance."
func CreateOnlyWarn(effect string) CreateOnlyModifier {
	return CreateOnlyModifier{effect: effect}
}

// Description describes the modifier for the schema.
func (m CreateOnlyModifier) Description(context.Context) string {
	return "Applied at creation only; a later change is stored without affecting the existing object."
}

// MarkdownDescription describes the modifier for the schema.
func (m CreateOnlyModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// warning returns the warning a change deserves, nil when it deserves none.
func (m CreateOnlyModifier) warning(creating, wasImported bool, stateValue, planValue attr.Value, p path.Path) diag.Diagnostics {
	if creating || wasImported || planValue.IsUnknown() || planValue.Equal(stateValue) {
		return nil
	}
	var diags diag.Diagnostics
	detail := fmt.Sprintf("%s is applied when the object is created only, so this change is stored in the state "+
		"but does not modify the existing object.", p)
	if m.effect != "" {
		detail += " " + m.effect
	}
	diags.AddAttributeWarning(p, "Change applies at creation only", detail)
	return diags
}

func (m CreateOnlyModifier) apply(ctx context.Context, state tfsdk.State, plan tfsdk.Plan, private privateReader, stateValue, planValue attr.Value, p path.Path, diags *diag.Diagnostics) {
	if plan.Raw.IsNull() {
		return
	}
	diags.Append(m.warning(state.Raw.IsNull(), imported(ctx, private), stateValue, planValue, p)...)
}

// PlanModifyString implements planmodifier.String.
func (m CreateOnlyModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	m.apply(ctx, req.State, req.Plan, req.Private, req.StateValue, req.PlanValue, req.Path, &resp.Diagnostics)
}

// PlanModifyBool implements planmodifier.Bool.
func (m CreateOnlyModifier) PlanModifyBool(ctx context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	m.apply(ctx, req.State, req.Plan, req.Private, req.StateValue, req.PlanValue, req.Path, &resp.Diagnostics)
}

// PlanModifyInt64 implements planmodifier.Int64.
func (m CreateOnlyModifier) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	m.apply(ctx, req.State, req.Plan, req.Private, req.StateValue, req.PlanValue, req.Path, &resp.Diagnostics)
}

// PlanModifySet implements planmodifier.Set.
func (m CreateOnlyModifier) PlanModifySet(ctx context.Context, req planmodifier.SetRequest, resp *planmodifier.SetResponse) {
	m.apply(ctx, req.State, req.Plan, req.Private, req.StateValue, req.PlanValue, req.Path, &resp.Diagnostics)
}

// PlanModifyList implements planmodifier.List.
func (m CreateOnlyModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	m.apply(ctx, req.State, req.Plan, req.Private, req.StateValue, req.PlanValue, req.Path, &resp.Diagnostics)
}

// PlanModifyMap implements planmodifier.Map.
func (m CreateOnlyModifier) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	m.apply(ctx, req.State, req.Plan, req.Private, req.StateValue, req.PlanValue, req.Path, &resp.Diagnostics)
}

// PlanModifyObject implements planmodifier.Object.
func (m CreateOnlyModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	m.apply(ctx, req.State, req.Plan, req.Private, req.StateValue, req.PlanValue, req.Path, &resp.Diagnostics)
}
