package hosting

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*phpVersionResource)(nil)
	_ resource.ResourceWithImportState = (*phpVersionResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*phpVersionResource)(nil)
)

// NewPHPVersionResource returns the onetsolutions_hosting_php_version resource.
func NewPHPVersionResource() resource.Resource {
	return &phpVersionResource{}
}

type phpVersionResource struct {
	common.ResourceBase
}

type phpVersionModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Domain         types.String `tfsdk:"domain"`
	PHPVersion     types.String `tfsdk:"php_version"`
}

func (m *phpVersionModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (r *phpVersionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_php_version"
}

func (r *phpVersionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the PHP version a domain of an OnetSolutions web hosting runs.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The domain, as `domain`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"hosting_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the web hosting. Changing it recreates the object in the other web hosting.",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"domain": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The domain of the web hosting, such as `blog.example.com`: its primary domain, a " +
					"subdomain or an addon domain. When unset, the primary domain. Changing it targets another domain " +
					"(replacement).",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(253),
					stringvalidator.RegexMatches(webHostingDomainPattern, webHostingDomainMessage),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"php_version": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The PHP version, as `onetsolutions_hosting_php_versions` lists it (such as " +
					"`ea-php83`), or `" + client.PHPVersionInherit + "` for the account's default version. Checked at plan " +
					"time against the versions the web hosting offers. Can be changed in place. With `" + client.PHPVersionInherit +
					"`, a version set outside Terraform afterwards is not detected.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 50)},
			},
		}),
	}
}

func (r *phpVersionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() || req.Plan.Raw.IsNull() || !r.Data().Ready() {
		return
	}
	var plan phpVersionModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !common.Known(plan.OrganizationID, plan.ProjectID, plan.HostingID, plan.PHPVersion) {
		return
	}
	version := plan.PHPVersion.ValueString()
	if version == client.PHPVersionInherit {
		return
	}
	if !req.State.Raw.IsNull() {
		var state phpVersionModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if state.PHPVersion.Equal(plan.PHPVersion) {
			return
		}
	}
	versions, err := r.Data().Client.ListHostingPHPVersions(ctx, plan.scope(), plan.HostingID.ValueString())
	if err != nil {
		resp.Diagnostics.AddWarning("Could not check the PHP version",
			"The PHP versions of the web hosting could not be listed: "+common.ErrorDetail(err))
		return
	}
	available := make([]string, 0, len(versions))
	for _, v := range versions {
		available = append(available, v.Version)
	}
	if !slices.Contains(available, version) {
		resp.Diagnostics.AddAttributeError(path.Root("php_version"), "PHP version not offered",
			fmt.Sprintf("Web hosting %s offers no PHP version %q. Versions offered: %s, or %s.", plan.HostingID.ValueString(),
				version, strings.Join(available, ", "), client.PHPVersionInherit))
	}
}

func (r *phpVersionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan phpVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := r.ResolveScope(ctx, plan.OrganizationID, plan.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	hostingID := plan.HostingID.ValueString()
	if !common.Known(plan.Domain) {
		hosting, err := c.GetWebHosting(ctx, scope, hostingID)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
			return
		}
		plan.Domain = types.StringValue(hosting.Domain)
	}
	if !r.set(ctx, c, &plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// set sends the PHP version of the plan, then reads it back.
func (r *phpVersionResource) set(ctx context.Context, c *client.Client, plan *phpVersionModel, diags *diag.Diagnostics) bool {
	scope, hostingID, domain := plan.scope(), plan.HostingID.ValueString(), plan.Domain.ValueString()
	err := c.SetHostingPHPVersion(ctx, scope, hostingID, client.SetPHPVersion{PHPVersion: plan.PHPVersion.ValueString(), Domain: domain})
	if err != nil {
		summary := "Could not set the PHP version of " + domain
		if client.IsForbidden(err) {
			diags.AddError(summary, common.ErrorDetail(err)+"\n\nThe PHP version of a domain can be set only while its web "+
				"hosting is active: once it is set up, and its order paid.")
			return false
		}
		diags.AddError(summary, common.ErrorDetail(err)+fmt.Sprintf("\n\nCheck that %s is a domain of web hosting %s.", domain, hostingID))
		return false
	}
	current, found, err := domainPHPVersion(ctx, c, scope, hostingID, domain)
	switch {
	case err != nil:
		diags.AddError("Could not read the PHP version of "+domain, common.ErrorDetail(err))
		return false
	case !found:
		diags.AddAttributeError(path.Root("domain"), "Unknown domain",
			fmt.Sprintf("Web hosting %s has no domain %s.", hostingID, domain))
		return false
	}
	plan.ID = plan.Domain
	plan.PHPVersion = types.StringValue(keptVersion(plan.PHPVersion.ValueString(), current))
	return true
}

// keptVersion is the version to store for a domain whose last known version was prior and that
// now runs current. A domain that inherits is listed with the version it runs, the account's
// default, so prior inherit is kept: it cannot be told apart from that version set explicitly.
func keptVersion(prior, current string) string {
	if prior == client.PHPVersionInherit {
		return prior
	}
	return current
}

// domainPHPVersion returns the PHP version of one domain of a web hosting, found false when the
// web hosting has no such domain.
func domainPHPVersion(ctx context.Context, c *client.Client, scope client.Scope, hostingID, domain string) (version string, found bool, err error) {
	versions, err := c.ListHostingDomainPHPVersions(ctx, scope, hostingID)
	if err != nil {
		return "", false, err
	}
	for _, v := range versions {
		if v.Domain == domain {
			return v.PHPVersion, true, nil
		}
	}
	return "", false, nil
}

func (r *phpVersionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state phpVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID := state.scope(), state.HostingID.ValueString()
	gone, err := webHostingGone(ctx, c, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	domain := state.ID.ValueString()
	version, found, err := domainPHPVersion(ctx, c, scope, hostingID, domain)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the PHP version of "+domain, err)
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	state.Domain = types.StringValue(domain)
	state.PHPVersion = types.StringValue(keptVersion(state.PHPVersion.ValueString(), version))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *phpVersionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state phpVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.OrganizationID, plan.ProjectID, plan.Domain = state.OrganizationID, state.ProjectID, state.Domain
	if !r.set(ctx, c, &plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete sets the domain back to the account's default PHP version. A domain gone with its web
// hosting is only forgotten.
func (r *phpVersionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state phpVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID, domain := state.scope(), state.HostingID.ValueString(), state.ID.ValueString()
	err := c.SetHostingPHPVersion(ctx, scope, hostingID, client.SetPHPVersion{PHPVersion: client.PHPVersionInherit, Domain: domain})
	switch {
	case err == nil, goneWithWebHosting(ctx, c, scope, hostingID, err):
	case client.IsForbidden(err):
		resp.Diagnostics.AddWarning("The PHP version was left as it is",
			fmt.Sprintf("Domain %s keeps PHP %s: its web hosting is not active, so the default version could not be "+
				"restored. The resource is no longer in the Terraform state.", domain, state.PHPVersion.ValueString()))
	default:
		common.AddClientError(&resp.Diagnostics, "Could not restore the default PHP version of "+domain, err)
	}
}

// ImportState imports "<organization_id>/<project_id>/<hosting_id>/<domain>" or
// "<hosting_id>/<domain>".
func (r *phpVersionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScopedChild(ctx, req, resp, "hosting_id")
	if resp.Diagnostics.HasError() {
		return
	}
	var id types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(common.AttrID), &id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), id)...)
}
