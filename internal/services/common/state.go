package common

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// SavePartialState writes id, organization_id, project_id and the extra attributes to the state
// of a resource being created, as soon as the API has returned the new object's ID and before
// any wait. When the wait then fails, times out or is interrupted, Create returns its error with
// this state set: Terraform keeps tracking the object as tainted, so a billed object is never
// orphaned and the next apply replaces it rather than ordering a second one.
func SavePartialState(ctx context.Context, state *tfsdk.State, id string, scope client.Scope, extra map[string]attr.Value) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(state.SetAttribute(ctx, path.Root(AttrID), id)...)
	diags.Append(state.SetAttribute(ctx, path.Root(AttrOrganizationID), scope.OrganizationID)...)
	diags.Append(state.SetAttribute(ctx, path.Root(AttrProjectID), scope.ProjectID)...)
	for name, value := range extra {
		diags.Append(state.SetAttribute(ctx, path.Root(name), value)...)
	}
	return diags
}

// Known reports whether every value is neither null nor unknown.
func Known(values ...attr.Value) bool {
	for _, v := range values {
		if v.IsNull() || v.IsUnknown() {
			return false
		}
	}
	return true
}
