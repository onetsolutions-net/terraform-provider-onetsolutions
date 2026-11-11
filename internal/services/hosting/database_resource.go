package hosting

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*databaseResource)(nil)
	_ resource.ResourceWithImportState = (*databaseResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*databaseResource)(nil)
)

// A database or database user name: letters, digits and underscores. The API limits what is sent
// to 16 characters, the account prefix included when it is written out.
var databaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func databaseNameValidators() []validator.String {
	return []validator.String{
		stringvalidator.LengthBetween(1, 16),
		stringvalidator.RegexMatches(databaseNamePattern, "must contain only letters, digits and underscores"),
	}
}

// NewDatabaseResource returns the onetsolutions_hosting_database resource.
func NewDatabaseResource() resource.Resource {
	return &databaseResource{}
}

type databaseResource struct {
	common.ResourceBase
}

type databaseModel struct {
	ID                 types.String `tfsdk:"id"`
	OrganizationID     types.String `tfsdk:"organization_id"`
	ProjectID          types.String `tfsdk:"project_id"`
	HostingID          types.String `tfsdk:"hosting_id"`
	Name               types.String `tfsdk:"name"`
	FullName           types.String `tfsdk:"full_name"`
	DeletionProtection types.Bool   `tfsdk:"deletion_protection"`
}

func (m *databaseModel) scope() client.Scope { return mailDBScope(m.OrganizationID, m.ProjectID) }

func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_database"
}

func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a MySQL database of a web hosting.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID:       mailDBIDAttribute("The full name of the database, the same as `full_name`."),
			mailDBHostingIDAttr: mailDBHostingAttribute(),
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The name of the database: letters, digits and underscores, at most 16 characters. " +
					"The web hosting's username and an underscore are prepended unless the name already starts with them " +
					"(see `full_name`); both spellings are equivalent. Changing it recreates the database, which deletes its data.",
				Validators: databaseNameValidators(),
			},
			"full_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The name applications connect to, prefixed with the web hosting's username, such as `acme_shop`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			mailDBProtectionAttr: schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying (or replacing) the database is refused. Defaults to `false`. Set it " +
					"to `false` and apply before destroying a protected database.",
			},
		}),
	}
}

func (r *databaseResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() || req.State.Raw.IsNull() {
		return
	}
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		if !state.DeletionProtection.ValueBool() {
			resp.Diagnostics.AddWarning("This plan deletes a database and its data",
				fmt.Sprintf("Destroying onetsolutions_hosting_database %s deletes the database and every table in it. This cannot be undone.",
					state.FullName.ValueString()))
		}
		return
	}
	var plan databaseModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	mailDBPlanName(ctx, r.Data(), state.scope(), state.HostingID.ValueString(), plan.Name, state.Name, state.FullName, "name", resp)
	replaced := len(resp.RequiresReplace) > 0 || (common.Known(plan.HostingID) && !plan.HostingID.Equal(state.HostingID))
	if replaced && !state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddWarning("This plan replaces a database and deletes its data",
			fmt.Sprintf("Replacing onetsolutions_hosting_database %s deletes the database and every table in it, then creates an empty one.",
				state.FullName.ValueString()))
	}
}

func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan databaseModel
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
	hostingID, name := plan.HostingID.ValueString(), plan.Name.ValueString()

	database, err := c.CreateHostingDatabase(ctx, scope, hostingID, name)
	if err != nil && mailDBMayHaveSucceeded(err) {
		database = r.find(ctx, c, scope, hostingID, name)
	}
	if database == nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the database", err, map[string]path.Path{
			"name": path.Root("name"),
		})
		return
	}
	plan.ID, plan.FullName = types.StringValue(database.Name), types.StringValue(database.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// find returns the database a create that failed without an answer may have made, nil if none.
func (r *databaseResource) find(ctx context.Context, c *client.Client, scope client.Scope, hostingID, name string) *client.HostingDatabase {
	hosting, err := c.GetWebHosting(ctx, scope, hostingID)
	if err != nil {
		return nil
	}
	databases, err := c.ListHostingDatabases(ctx, scope, hostingID)
	if err != nil {
		return nil
	}
	full := mailDBPrefixed(hosting.Username, name)
	for i := range databases {
		if databases[i].Name == full {
			return &databases[i]
		}
	}
	return nil
}

func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state databaseModel
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
	databases, err := c.ListHostingDatabases(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the databases", err)
		return
	}
	full := mailDBPrefixed(hosting.Username, state.ID.ValueString())
	found := false
	for _, database := range databases {
		found = found || database.Name == full
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID, state.FullName = types.StringValue(full), types.StringValue(full)
	if state.Name.IsNull() {
		state.Name = types.StringValue(mailDBShort(hosting.Username, full))
	}
	if state.DeletionProtection.IsNull() {
		state.DeletionProtection = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only records a new spelling of the name, or deletion_protection.
func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID, plan.FullName = state.ID, state.FullName
	plan.OrganizationID, plan.ProjectID = state.OrganizationID, state.ProjectID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.DeletionProtection.ValueBool() {
		mailDBDeletionProtectionError(&resp.Diagnostics, "database", state.FullName.ValueString())
		return
	}
	scope, hostingID, full := state.scope(), state.HostingID.ValueString(), state.FullName.ValueString()
	err := c.DeleteHostingDatabase(ctx, scope, hostingID, full)
	if err == nil || mailDBGoneWithHosting(ctx, c, scope, hostingID, err) {
		return
	}
	list := func(ctx context.Context) ([]client.HostingDatabase, error) {
		return c.ListHostingDatabases(ctx, scope, hostingID)
	}
	if client.IsCanceled(err) || !mailDBAbsent(ctx, list, func(d client.HostingDatabase) bool { return d.Name == full }) {
		common.AddClientError(&resp.Diagnostics, "Could not delete the database", err)
	}
}

func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	mailDBImport(ctx, &r.ResourceBase, req, resp, 1, "<name>")
}
