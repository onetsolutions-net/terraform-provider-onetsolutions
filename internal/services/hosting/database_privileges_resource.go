package hosting

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*databasePrivilegesResource)(nil)
	_ resource.ResourceWithImportState = (*databasePrivilegesResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*databasePrivilegesResource)(nil)
)

// databasePrivileges are the privileges a user can be granted on a database, besides
// ALL PRIVILEGES.
var databasePrivileges = []string{
	"ALTER", "ALTER ROUTINE", "CREATE", "CREATE ROUTINE", "CREATE TEMPORARY TABLES", "CREATE VIEW", "DELETE", "DROP",
	"EVENT", "EXECUTE", "INDEX", "INSERT", "LOCK TABLES", "REFERENCES", "SELECT", "SHOW VIEW", "TRIGGER", "UPDATE",
}

// NewDatabasePrivilegesResource returns the onetsolutions_hosting_database_privileges resource.
func NewDatabasePrivilegesResource() resource.Resource {
	return &databasePrivilegesResource{}
}

type databasePrivilegesResource struct {
	common.ResourceBase
}

type databasePrivilegesModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Database       types.String `tfsdk:"database"`
	Username       types.String `tfsdk:"username"`
	Privileges     types.Set    `tfsdk:"privileges"`
}

func (m *databasePrivilegesModel) scope() client.Scope {
	return mailDBScope(m.OrganizationID, m.ProjectID)
}

// keys returns the full database and user names of the ID, "<database>/<username>".
func (m *databasePrivilegesModel) keys() (database, username string) {
	database, username, _ = strings.Cut(m.ID.ValueString(), "/")
	return database, username
}

func (r *databasePrivilegesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_database_privileges"
}

func (r *databasePrivilegesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants a MySQL user of a web hosting privileges on one of its databases.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: mailDBIDAttribute("The full database name and the full username, separated by a slash: " +
				"`<database>/<username>`."),
			mailDBHostingIDAttr: mailDBHostingAttribute(),
			"database": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The name of the database, with or without the web hosting's username prefix, " +
					"such as `onetsolutions_hosting_database.shop.name`. Changing it grants the user on another database (replacement).",
				Validators: databaseNameValidators(),
			},
			"username": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The name of the user, with or without the web hosting's username prefix, such as " +
					"`onetsolutions_hosting_database_user.app.username`. Changing it grants another user (replacement).",
				Validators: databaseNameValidators(),
			},
			"privileges": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default: setdefault.StaticValue(types.SetValueMust(types.StringType,
					[]attr.Value{types.StringValue(mailDBAllPrivileges)})),
				MarkdownDescription: "The privileges granted: `ALL PRIVILEGES` (the default), or any of `" +
					strings.Join(databasePrivileges, "`, `") + "`. They replace the user's previous privileges on " +
					"the database and can be changed in place.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(append([]string{mailDBAllPrivileges}, databasePrivileges...)...)),
					allPrivilegesAlone{},
				},
			},
		}),
	}
}

func (r *databasePrivilegesResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() || req.State.Raw.IsNull() {
		return
	}
	var state databasePrivilegesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	database, username := state.keys()
	if req.Plan.Raw.IsNull() {
		resp.Diagnostics.AddWarning("Destroying database privileges does not revoke them",
			fmt.Sprintf("The privileges of %s on %s are only removed from the Terraform state: they stay granted until "+
				"the user or the database is deleted, or other privileges are set.", username, database))
		return
	}
	var plan databasePrivilegesModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID := state.scope(), state.HostingID.ValueString()
	mailDBPlanName(ctx, r.Data(), scope, hostingID, plan.Database, state.Database, types.StringValue(database), "database", resp)
	mailDBPlanName(ctx, r.Data(), scope, hostingID, plan.Username, state.Username, types.StringValue(username), "username", resp)
}

func (r *databasePrivilegesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databasePrivilegesModel
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
	hosting, err := c.GetWebHosting(ctx, scope, plan.HostingID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return
	}
	database := mailDBPrefixed(hosting.Username, plan.Database.ValueString())
	username := mailDBPrefixed(hosting.Username, plan.Username.ValueString())
	plan.ID = types.StringValue(database + "/" + username)
	if !r.set(ctx, c, &plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// set sends the privileges of the plan; the API replaces what the user had on the database.
func (r *databasePrivilegesResource) set(ctx context.Context, c *client.Client, plan *databasePrivilegesModel, diags *diag.Diagnostics) bool {
	privileges, ok := privilegesString(ctx, plan.Privileges)
	if !ok {
		diags.AddAttributeError(path.Root("privileges"), "Invalid privileges", "The privileges could not be read.")
		return false
	}
	database, username := plan.keys()
	err := c.SetHostingDatabasePrivileges(ctx, plan.scope(), plan.HostingID.ValueString(), username, database, privileges)
	if err != nil {
		common.AddClientErrorWithFields(diags, fmt.Sprintf("Could not grant %s on %s", username, database), err, map[string]path.Path{
			"username": path.Root("username"), "database": path.Root("database"), "privileges": path.Root("privileges"),
		})
		return false
	}
	return true
}

func (r *databasePrivilegesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databasePrivilegesModel
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
	database, username := state.keys()
	database, username = mailDBPrefixed(hosting.Username, database), mailDBPrefixed(hosting.Username, username)
	databases, err := c.ListHostingDatabases(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the databases", err)
		return
	}
	granted := false
	for _, d := range databases {
		granted = granted || (d.Name == database && slices.Contains(d.Users, username))
	}
	if !granted {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = types.StringValue(database + "/" + username)
	if state.Database.IsNull() {
		state.Database = types.StringValue(mailDBShort(hosting.Username, database))
	}
	if state.Username.IsNull() {
		state.Username = types.StringValue(mailDBShort(hosting.Username, username))
	}
	if state.Privileges.IsNull() {
		state.Privileges = types.SetNull(types.StringType)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *databasePrivilegesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state databasePrivilegesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID, plan.OrganizationID, plan.ProjectID = state.ID, state.OrganizationID, state.ProjectID
	if !plan.Privileges.Equal(state.Privileges) && !r.set(ctx, c, &plan, &resp.Diagnostics) {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete only forgets the privileges: the API has no way to revoke them.
func (r *databasePrivilegesResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *databasePrivilegesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	mailDBImport(ctx, &r.ResourceBase, req, resp, 2, "<database>/<username>")
}

// allPrivilegesAlone refuses ALL PRIVILEGES next to other privileges.
type allPrivilegesAlone struct{}

func (allPrivilegesAlone) Description(context.Context) string {
	return "ALL PRIVILEGES cannot be combined with other privileges"
}

func (v allPrivilegesAlone) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (allPrivilegesAlone) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var values []types.String
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &values, false)...)
	if len(values) > 1 && slices.ContainsFunc(values, func(v types.String) bool { return v.ValueString() == mailDBAllPrivileges }) {
		resp.Diagnostics.AddAttributeError(req.Path, "Conflicting privileges",
			"ALL PRIVILEGES already includes every other privilege: set it alone, or list the privileges to grant.")
	}
}

// privilegesString joins privileges the way the API takes them, sorted for a stable request.
func privilegesString(ctx context.Context, set types.Set) (string, bool) {
	var values []string
	if set.ElementsAs(ctx, &values, false).HasError() {
		return "", false
	}
	slices.Sort(values)
	return strings.Join(values, ","), true
}
