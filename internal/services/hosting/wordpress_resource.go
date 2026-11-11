package hosting

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*wordPressResource)(nil)
	_ resource.ResourceWithImportState = (*wordPressResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*wordPressResource)(nil)
)

const wordPressCreateOnlyEffect = "It applies when the site is installed only; change it in WordPress itself."

// wordPressPathPattern is empty or a directory under the domain's root, without leading or
// trailing slash.
var wordPressPathPattern = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*)?$`)

// NewWordPressResource returns the onetsolutions_hosting_wordpress resource.
func NewWordPressResource() resource.Resource {
	return &wordPressResource{}
}

type wordPressResource struct {
	common.ResourceBase
}

type wordPressModel struct {
	ID                 types.String `tfsdk:"id"`
	OrganizationID     types.String `tfsdk:"organization_id"`
	ProjectID          types.String `tfsdk:"project_id"`
	HostingID          types.String `tfsdk:"hosting_id"`
	Domain             types.String `tfsdk:"domain"`
	Path               types.String `tfsdk:"path"`
	Title              types.String `tfsdk:"title"`
	Language           types.String `tfsdk:"language"`
	AdminUser          types.String `tfsdk:"admin_user"`
	AdminPassword      types.String `tfsdk:"admin_password"`
	AdminEmail         types.String `tfsdk:"admin_email"`
	UpdateTrigger      types.String `tfsdk:"update_trigger"`
	DeletionProtection types.Bool   `tfsdk:"deletion_protection"`
	Version            types.String `tfsdk:"version"`
}

func (m *wordPressModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (m *wordPressModel) fill(site *client.WordPressSite) {
	m.ID = types.StringValue(site.ID)
	m.Domain = types.StringValue(site.Domain)
	m.Path = types.StringValue(strings.Trim(site.Path, "/"))
	m.Version = types.StringValue(site.Version)
	if m.DeletionProtection.IsNull() || m.DeletionProtection.IsUnknown() {
		m.DeletionProtection = types.BoolValue(false)
	}
}

func (r *wordPressResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_wordpress"
}

func (r *wordPressResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	createOnly := []planmodifier.String{common.CreateOnlyWarn(wordPressCreateOnlyEffect)}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a WordPress site installed on an OnetSolutions web hosting.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The identifier of the WordPress site on its web hosting, such as `26_40001`, as " +
					"`onetsolutions_hosting_wordpress_sites` lists it; it is the last part of the import ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"hosting_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the web hosting. Changing it replaces the site.",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       replace,
			},
			"domain": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The domain of the web hosting the site is installed on: its primary domain, a " +
					"subdomain or an addon domain. Changing it replaces the site.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(253),
					stringvalidator.RegexMatches(webHostingDomainPattern, webHostingDomainMessage),
				},
				PlanModifiers: replace,
			},
			"path": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(""),
				MarkdownDescription: "The directory under the domain the site is installed in, such as `blog`, without " +
					"leading or trailing slash. Defaults to `\"\"`, the root of the domain. Changing it replaces the site.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(255),
					stringvalidator.RegexMatches(wordPressPathPattern, "must be empty or a directory such as blog or sites/blog, without leading or trailing slash"),
				},
				PlanModifiers: replace,
			},
			"title": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The site's title. When unset, `WordPress`. Applied at installation only.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
				PlanModifiers:       createOnly,
			},
			"language": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The site's language code, such as `fr` or `en`. When unset, `en`. Applied at installation only.",
				Validators:          []validator.String{stringvalidator.LengthBetween(2, 10)},
				PlanModifiers:       createOnly,
			},
			"admin_user": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The login of the site's administrator, required to install the site. Applied at " +
					"installation only.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 60)},
				PlanModifiers: createOnly,
			},
			"admin_password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "The password of the site's administrator, required to install the site. It is kept " +
					"in the Terraform state, so protect the state. Applied at installation only: changing it does not " +
					"change the password. At least 8 characters, with an uppercase letter, a lowercase letter, a digit and " +
					"a special character.",
				Validators:    []validator.String{stringvalidator.LengthBetween(8, 128), mailDBStrongPassword{}},
				PlanModifiers: createOnly,
			},
			"admin_email": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The e-mail address of the site's administrator, required to install the site. " +
					"Applied at installation only.",
				Validators: []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`),
					"must be an e-mail address")},
				PlanModifiers: createOnly,
			},
			"update_trigger": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Any value: changing it updates the site to the latest WordPress version, in place. " +
					"Not used when the site is installed, which installs the latest version.",
			},
			"deletion_protection": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying (or replacing) the site is refused. Defaults to `false`. Set it to " +
					"`false` and apply before destroying a protected site.",
			},
			"version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The installed WordPress version.",
			},
		}),
	}
}

func (r *wordPressResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		var state wordPressModel
		if req.State.Raw.IsNull() || req.State.Get(ctx, &state).HasError() || state.DeletionProtection.ValueBool() {
			return
		}
		resp.Diagnostics.AddWarning("This plan deletes a WordPress site", fmt.Sprintf(
			"The WordPress site %s on %s is deleted with its files and its database. Back it up first if you need its content.",
			state.ID.ValueString(), siteURL(&state)))
		return
	}
	var plan wordPressModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.State.Raw.IsNull() {
		for name, value := range map[string]types.String{"admin_user": plan.AdminUser, "admin_password": plan.AdminPassword, "admin_email": plan.AdminEmail} {
			if value.IsNull() {
				resp.Diagnostics.AddAttributeError(path.Root(name), name+" is required to install the site",
					"Set admin_user, admin_password and admin_email: the site's administrator account is created with them.")
			}
		}
		return
	}
	var state wordPressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	replaced := !plan.HostingID.Equal(state.HostingID) || !plan.Domain.Equal(state.Domain) || !plan.Path.Equal(state.Path)
	if replaced && !state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddWarning("Replacing this WordPress site deletes its content", fmt.Sprintf(
			"The plan replaces the WordPress site %s on %s: the site is deleted with its files and its database, and a new, "+
				"empty site is installed. Back it up first if you need its content.", state.ID.ValueString(), siteURL(&state)))
	}
}

func siteURL(m *wordPressModel) string {
	if p := m.Path.ValueString(); p != "" {
		return m.Domain.ValueString() + "/" + p
	}
	return m.Domain.ValueString()
}

func (r *wordPressResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan wordPressModel
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
	installed, err := c.InstallHostingWordPress(ctx, scope, hostingID, client.WordPressInstall{
		Domain:        plan.Domain.ValueString(),
		Path:          plan.Path.ValueString(),
		AdminUser:     plan.AdminUser.ValueString(),
		AdminPassword: plan.AdminPassword.ValueString(),
		AdminEmail:    plan.AdminEmail.ValueString(),
		Title:         plan.Title.ValueString(),
		Language:      plan.Language.ValueString(),
	})
	if client.IsForbidden(err) {
		resp.Diagnostics.AddError("Could not install WordPress", common.ErrorDetail(err)+"\n\nWordPress can be "+
			"installed only while its web hosting is active: once it is set up, and its order paid.")
		return
	}
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not install WordPress", err,
			map[string]path.Path{
				"domain": path.Root("domain"), "path": path.Root("path"), "admin_user": path.Root("admin_user"),
				"admin_password": path.Root("admin_password"), "admin_email": path.Root("admin_email"),
				"title": path.Root("title"), "language": path.Root("language"),
			})
		return
	}
	site, err := r.findSite(ctx, c, &plan, installed.ID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "WordPress was installed but could not be read", err)
		return
	}
	if site == nil {
		resp.Diagnostics.AddError("WordPress was installed but cannot be found", fmt.Sprintf(
			"The site on %s was installed, but web hosting %s does not list it. Import it (terraform import) once it is "+
				"listed, or delete it in the console, before applying again.", siteURL(&plan), hostingID))
		return
	}
	plan.fill(site)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// findSite returns the site of the hosting with id, or, when id is empty, the one on the plan's
// domain and path; nil when none is listed.
func (r *wordPressResource) findSite(ctx context.Context, c *client.Client, m *wordPressModel, id string) (*client.WordPressSite, error) {
	sites, err := c.ListHostingWordPressSites(ctx, m.scope(), m.HostingID.ValueString())
	if err != nil {
		return nil, err
	}
	for i := range sites {
		site := &sites[i]
		if id != "" && site.ID == id {
			return site, nil
		}
		if id == "" && site.Domain == m.Domain.ValueString() && strings.Trim(site.Path, "/") == m.Path.ValueString() {
			return site, nil
		}
	}
	return nil, nil
}

func (r *wordPressResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state wordPressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := webHostingGone(ctx, c, state.scope(), state.HostingID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	site, err := r.findSite(ctx, c, &state, state.ID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the WordPress site", err)
		return
	}
	if site == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(site)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *wordPressResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state wordPressModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID, plan.OrganizationID, plan.ProjectID = state.ID, state.OrganizationID, state.ProjectID
	if !plan.UpdateTrigger.Equal(state.UpdateTrigger) && !plan.UpdateTrigger.IsNull() {
		if err := c.UpdateHostingWordPress(ctx, plan.scope(), plan.HostingID.ValueString(), plan.ID.ValueString()); err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not update WordPress", err)
			return
		}
	}
	site, err := r.findSite(ctx, c, &plan, plan.ID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the WordPress site", err)
		return
	}
	if site == nil {
		resp.Diagnostics.AddError("The WordPress site is gone",
			fmt.Sprintf("Web hosting %s no longer lists the WordPress site %s.", plan.HostingID.ValueString(), plan.ID.ValueString()))
		return
	}
	plan.fill(site)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(common.ClearImported(ctx, resp.Private)...)
}

// Delete deletes the site with its files and its database. A site gone with its web hosting is
// only forgotten.
func (r *wordPressResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state wordPressModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddError("The WordPress site is protected from deletion",
			fmt.Sprintf("The WordPress site %s on %s has deletion_protection = true. Set it to false and apply before "+
				"destroying or replacing it.", state.ID.ValueString(), siteURL(&state)))
		return
	}
	scope, hostingID := state.scope(), state.HostingID.ValueString()
	err := c.DeleteHostingWordPress(ctx, scope, hostingID, state.ID.ValueString())
	if err != nil && !goneWithWebHosting(ctx, c, scope, hostingID, err) {
		common.AddClientError(&resp.Diagnostics, "Could not delete the WordPress site", err)
	}
}

// ImportState imports "<organization_id>/<project_id>/<hosting_id>/<id>" or "<hosting_id>/<id>",
// and marks the site imported, so its installation inputs are taken from the configuration
// without a warning.
func (r *wordPressResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScopedChild(ctx, req, resp, "hosting_id")
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(common.MarkImported(ctx, resp.Private)...)
	}
}
