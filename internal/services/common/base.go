package common

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// ResourceBase is embedded by every resource. It receives the provider data in Configure and,
// through ModifyPlan, fills organization_id/project_id on create (see PlanScope). A resource
// that defines its own ModifyPlan calls PlanScope first.
type ResourceBase struct {
	data *ProviderData
}

// Configure stores the provider data.
func (b *ResourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	b.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// Data returns the provider data, nil before Configure (e.g. during validate).
func (b *ResourceBase) Data() *ProviderData { return b.data }

// Client returns the API client, or adds an error diagnostic and returns nil.
func (b *ResourceBase) Client(diags *diag.Diagnostics) *client.Client {
	return b.data.RequireClient(diags)
}

// ResolveScope resolves the scope of an object being created; see ProviderData.ResolveScope.
func (b *ResourceBase) ResolveScope(ctx context.Context, organizationID, projectID types.String) (client.Scope, diag.Diagnostics) {
	var diags diag.Diagnostics
	if b.data.RequireClient(&diags) == nil {
		return client.Scope{}, diags
	}
	return b.data.ResolveScope(ctx, organizationID, projectID)
}

// ModifyPlan applies PlanScope.
func (b *ResourceBase) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	b.PlanScope(ctx, req, resp)
}

// PlanScope sets organization_id and project_id in the plan of a resource being created when
// its configuration leaves them unset, so the plan shows where the object will be created and
// plan-time API calls (price previews) know the scope. Lazy resolution therefore only runs for
// creates. Updates keep the values of the state (ScopeAttributes' plan modifiers).
func (b *ResourceBase) PlanScope(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || !req.State.Raw.IsNull() || !b.data.Ready() {
		return
	}
	var configOrg, configProject types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(AttrOrganizationID), &configOrg)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(AttrProjectID), &configProject)...)
	if resp.Diagnostics.HasError() || configOrg.IsUnknown() || configProject.IsUnknown() {
		return
	}
	scope, diags := b.data.ResolveScope(ctx, configOrg, configProject)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(AttrOrganizationID), scope.OrganizationID)...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(AttrProjectID), scope.ProjectID)...)
}

// DataSourceBase is embedded by every data source.
type DataSourceBase struct {
	data *ProviderData
}

// Configure stores the provider data.
func (b *DataSourceBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	b.data = providerDataFrom(req.ProviderData, &resp.Diagnostics)
}

// Data returns the provider data, nil before Configure.
func (b *DataSourceBase) Data() *ProviderData { return b.data }

// Client returns the API client, or adds an error diagnostic and returns nil.
func (b *DataSourceBase) Client(diags *diag.Diagnostics) *client.Client {
	return b.data.RequireClient(diags)
}

// ResolveScope resolves the scope a data source reads from; see ProviderData.ResolveScope.
func (b *DataSourceBase) ResolveScope(ctx context.Context, organizationID, projectID types.String) (client.Scope, diag.Diagnostics) {
	var diags diag.Diagnostics
	if b.data.RequireClient(&diags) == nil {
		return client.Scope{}, diags
	}
	return b.data.ResolveScope(ctx, organizationID, projectID)
}

// ResolveOrganization resolves the organization a data source reads from.
func (b *DataSourceBase) ResolveOrganization(ctx context.Context, organizationID types.String) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if b.data.RequireClient(&diags) == nil {
		return "", diags
	}
	return b.data.ResolveOrganization(ctx, organizationID)
}

func providerDataFrom(raw any, diags *diag.Diagnostics) *ProviderData {
	if raw == nil {
		return nil
	}
	data, ok := raw.(*ProviderData)
	if !ok {
		diags.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *common.ProviderData, got %T. This is a bug in the provider; please report it.", raw))
		return nil
	}
	return data
}
