package hosting

import (
	"context"
	"fmt"

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
	_ resource.ResourceWithConfigure   = (*databaseUserResource)(nil)
	_ resource.ResourceWithImportState = (*databaseUserResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*databaseUserResource)(nil)
)

// NewDatabaseUserResource returns the onetsolutions_hosting_database_user resource.
func NewDatabaseUserResource() resource.Resource {
	return &databaseUserResource{}
}

type databaseUserResource struct {
	common.ResourceBase
}

type databaseUserModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Username       types.String `tfsdk:"username"`
	FullUsername   types.String `tfsdk:"full_username"`
	Password       types.String `tfsdk:"password"`
}

func (m *databaseUserModel) scope() client.Scope { return mailDBScope(m.OrganizationID, m.ProjectID) }

func (r *databaseUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_database_user"
}

func (r *databaseUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a MySQL user of a web hosting.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID:       mailDBIDAttribute("The full username, the same as `full_username`."),
			mailDBHostingIDAttr: mailDBHostingAttribute(),
			"username": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The name of the user: letters, digits and underscores, at most 16 characters. The " +
					"web hosting's username and an underscore are prepended unless the name already starts with them " +
					"(see `full_username`); both spellings are equivalent. Changing it recreates the user.",
				Validators: databaseNameValidators(),
			},
			"full_username": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The username applications connect with, prefixed with the web hosting's username, such as `acme_app`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"password": mailDBPasswordAttribute("The password of the user. A password cannot be changed in place: changing " +
				"it recreates the user, which drops its privileges (see the page's notes)."),
		}),
	}
}

func (r *databaseUserResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() || req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state databaseUserModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	mailDBPlanName(ctx, r.Data(), state.scope(), state.HostingID.ValueString(), plan.Username, state.Username, state.FullUsername, "username", resp)
	if plan.Password.Equal(state.Password) {
		return
	}
	if state.Password.IsNull() {
		resp.Diagnostics.AddAttributeWarning(path.Root("password"), "Password recorded, not verified",
			fmt.Sprintf("The password of database user %s cannot be read back, so the configured one is recorded in the "+
				"state as it is, without changing the user. Change it in the configuration to set a new one.", state.FullUsername.ValueString()))
		return
	}
	resp.RequiresReplace = append(resp.RequiresReplace, path.Root("password"))
	resp.Diagnostics.AddAttributeWarning(path.Root("password"), "A new password recreates the database user",
		fmt.Sprintf("Database user %s is deleted and created again with the new password, which drops its privileges: "+
			"onetsolutions_hosting_database_privileges grants them again only when it is replaced too, for instance "+
			"through replace_triggered_by.", state.FullUsername.ValueString()))
}

func (r *databaseUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databaseUserModel
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
	plan.OrganizationID, plan.ProjectID = types.StringValue(scope.OrganizationID), types.StringValue(scope.ProjectID)
	hostingID, username := plan.HostingID.ValueString(), plan.Username.ValueString()

	user, err := c.CreateHostingDatabaseUser(ctx, scope, hostingID, username, plan.Password.ValueString())
	if err != nil && mailDBMayHaveSucceeded(err) {
		user = r.find(ctx, c, scope, hostingID, username)
	}
	if user == nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the database user", err, map[string]path.Path{
			"username": path.Root("username"), "password": path.Root("password"),
		})
		return
	}
	plan.ID, plan.FullUsername = types.StringValue(user.Username), types.StringValue(user.Username)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *databaseUserResource) find(ctx context.Context, c *client.Client, scope client.Scope, hostingID, username string) *client.HostingDatabaseUser {
	hosting, err := c.GetWebHosting(ctx, scope, hostingID)
	if err != nil {
		return nil
	}
	users, err := c.ListHostingDatabaseUsers(ctx, scope, hostingID)
	if err != nil {
		return nil
	}
	full := mailDBPrefixed(hosting.Username, username)
	for i := range users {
		if users[i].Username == full {
			return &users[i]
		}
	}
	return nil
}

func (r *databaseUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databaseUserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID := state.scope(), state.HostingID.ValueString()
	hosting, ok := mailDBReadChild(ctx, c, scope, hostingID, resp)
	if !ok {
		return
	}
	users, err := c.ListHostingDatabaseUsers(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the database users", err)
		return
	}
	full := mailDBPrefixed(hosting.Username, state.ID.ValueString())
	found := false
	for _, user := range users {
		found = found || user.Username == full
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID, state.FullUsername = types.StringValue(full), types.StringValue(full)
	if state.Username.IsNull() {
		state.Username = types.StringValue(mailDBShort(hosting.Username, full))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update records a new spelling of the username, or the password of an imported user.
func (r *databaseUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state databaseUserModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID, plan.FullUsername = state.ID, state.FullUsername
	plan.OrganizationID, plan.ProjectID = state.OrganizationID, state.ProjectID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *databaseUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseUserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID, full := state.scope(), state.HostingID.ValueString(), state.FullUsername.ValueString()
	err := c.DeleteHostingDatabaseUser(ctx, scope, hostingID, full)
	if err == nil || mailDBGoneWithHosting(ctx, c, scope, hostingID, err) {
		return
	}
	list := func(ctx context.Context) ([]client.HostingDatabaseUser, error) {
		return c.ListHostingDatabaseUsers(ctx, scope, hostingID)
	}
	if client.IsCanceled(err) || !mailDBAbsent(ctx, list, func(u client.HostingDatabaseUser) bool { return u.Username == full }) {
		common.AddClientError(&resp.Diagnostics, "Could not delete the database user", err)
	}
}

func (r *databaseUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	mailDBImport(ctx, &r.ResourceBase, req, resp, 1, "<username>")
}

// mailDBPasswordAttribute is a password the API takes but never returns: it lives in the state only.
func mailDBPasswordAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Required:  true,
		Sensitive: true,
		MarkdownDescription: description + " At least 8 characters with an uppercase letter, a lowercase letter, a " +
			"digit and a special character. It is never read back: it is stored in the state, and an import cannot recover it.",
		Validators: []validator.String{mailDBStrongPassword{}},
	}
}
