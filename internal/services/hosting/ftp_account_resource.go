package hosting

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*ftpAccountResource)(nil)
	_ resource.ResourceWithImportState = (*ftpAccountResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*ftpAccountResource)(nil)
)

const defaultFTPDirectory = "/public_html"

var (
	ftpUsernamePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	ftpDirectoryPattern = regexp.MustCompile(`^/(?:[^/]+(?:/[^/]+)*)?$`)
)

// NewFTPAccountResource returns the onetsolutions_hosting_ftp_account resource.
func NewFTPAccountResource() resource.Resource {
	return &ftpAccountResource{}
}

type ftpAccountResource struct {
	common.ResourceBase
}

type ftpAccountModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Username       types.String `tfsdk:"username"`
	Password       types.String `tfsdk:"password"`
	Directory      types.String `tfsdk:"directory"`
	QuotaMB        types.Int64  `tfsdk:"quota_mb"`
	Login          types.String `tfsdk:"login"`
	HomeDirectory  types.String `tfsdk:"home_directory"`
}

func (m *ftpAccountModel) scope() client.Scope { return mailDBScope(m.OrganizationID, m.ProjectID) }

func (r *ftpAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_ftp_account"
}

func (r *ftpAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an FTP account of a web hosting, confined to one directory of it.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID:       mailDBIDAttribute("The username of the account, the same as `username`."),
			mailDBHostingIDAttr: mailDBHostingAttribute(),
			"username": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The username of the account: lowercase letters, digits, `.`, `_` and `-`, at most " +
					"32 characters. Changing it recreates the account.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 32),
					stringvalidator.RegexMatches(ftpUsernamePattern,
						"must start with a lowercase letter or a digit and contain only lowercase letters, digits, '.', '_' and '-'"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password": mailDBPasswordAttribute("The password of the account. Can be changed in place; after an import, " +
				"the next apply sets the account's password to the configured one."),
			"directory": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(defaultFTPDirectory),
				MarkdownDescription: "The directory the account is confined to, relative to the web hosting's home " +
					"directory, starting with `/` and without a trailing `/`, such as `/public_html/uploads`. Defaults to `" +
					defaultFTPDirectory + "`. Changing it recreates the account; the files stay.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(ftpDirectoryPattern, "must start with / and have no trailing / nor empty segment"),
					noParentSegment{},
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"quota_mb": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(0),
				MarkdownDescription: "The disk quota of the account in MB; `0` (the default) means unlimited. Changing it " +
					"recreates the account; the files stay.",
				Validators:    []validator.Int64{int64validator.AtLeast(0)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"login": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The login to connect with, the username followed by `@` and the web hosting's domain.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"home_directory": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The absolute path of the account's directory on the web hosting.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
	}
}

func (r *ftpAccountResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() || req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state ftpAccountModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || ftpAccountReplaced(&plan, &state) {
		return
	}
	if state.Password.IsNull() && common.Known(plan.Password) {
		resp.Diagnostics.AddAttributeWarning(path.Root("password"), "The FTP account's password will be set",
			fmt.Sprintf("The password of FTP account %s cannot be read back, so this apply sets it to the configured one.",
				state.ID.ValueString()))
	}
}

// ftpAccountReplaced reports whether the plan replaces the account. resp.RequiresReplace cannot
// say: inside a resource's ModifyPlan it never holds the attributes' RequiresReplace modifiers.
func ftpAccountReplaced(plan, state *ftpAccountModel) bool {
	return !plan.Username.Equal(state.Username) || !plan.Directory.Equal(state.Directory) ||
		!plan.QuotaMB.Equal(state.QuotaMB) || !plan.HostingID.Equal(state.HostingID) ||
		!plan.OrganizationID.Equal(state.OrganizationID) || !plan.ProjectID.Equal(state.ProjectID)
}

func (r *ftpAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ftpAccountModel
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
	hosting, err := c.GetWebHosting(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return
	}

	_, err = c.CreateHostingFTPAccount(ctx, scope, hostingID, client.CreateHostingFTPAccountRequest{
		Username: username, Password: plan.Password.ValueString(),
		Directory: plan.Directory.ValueString(), QuotaMB: plan.QuotaMB.ValueInt64(),
	})
	if err != nil && !mailDBMayHaveSucceeded(err) {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the FTP account", err, map[string]path.Path{
			"username": path.Root("username"), "password": path.Root("password"),
			"directory": path.Root("directory"), "quota_mb": path.Root("quota_mb"),
		})
		return
	}
	account, readErr := findFTPAccount(ctx, c, scope, hosting, username)
	if account == nil {
		if err == nil {
			err = readErr
		}
		if err == nil {
			err = fmt.Errorf("FTP account %s is not listed after its creation", username)
		}
		common.AddClientError(&resp.Diagnostics, "Could not create the FTP account", err)
		return
	}
	plan.ID = types.StringValue(username)
	plan.fill(hosting, account)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (m *ftpAccountModel) fill(hosting *client.WebHosting, account *client.HostingFTPAccount) {
	login := account.User
	if !strings.Contains(login, "@") {
		login += "@" + hosting.Domain
	}
	m.Login = types.StringValue(login)
	m.HomeDirectory = types.StringValue(account.Directory)
}

// findFTPAccount returns the FTP account of that username, nil when there is none. The list may
// name an account by its username or by its full login.
func findFTPAccount(ctx context.Context, c *client.Client, scope client.Scope, hosting *client.WebHosting, username string) (*client.HostingFTPAccount, error) {
	accounts, err := c.ListHostingFTPAccounts(ctx, scope, hosting.ID)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].User == username || accounts[i].User == username+"@"+hosting.Domain {
			return &accounts[i], nil
		}
	}
	return nil, nil
}

// relativeFTPDirectory turns an absolute directory back into the form the account was created
// with, relative to the home of the hosting account; "" when it is outside it.
func relativeFTPDirectory(username, absolute string) string {
	marker := "/" + username
	index := strings.Index(absolute+"/", marker+"/")
	if index < 0 {
		return ""
	}
	relative := strings.TrimSuffix(absolute[index+len(marker):], "/")
	if relative == "" {
		return "/"
	}
	return relative
}

func (r *ftpAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ftpAccountModel
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
	account, err := findFTPAccount(ctx, c, scope, hosting, state.ID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the FTP accounts", err)
		return
	}
	if account == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(hosting, account)
	state.QuotaMB = types.Int64Value(account.QuotaMB)
	if state.Username.IsNull() {
		state.Username = state.ID
	}
	if state.Directory.IsNull() {
		if relative := relativeFTPDirectory(hosting.Username, account.Directory); relative != "" {
			state.Directory = types.StringValue(relative)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update changes the password, the only setting the API can change in place.
func (r *ftpAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ftpAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID, plan.Login, plan.HomeDirectory = state.ID, state.Login, state.HomeDirectory
	plan.OrganizationID, plan.ProjectID = state.OrganizationID, state.ProjectID
	if !plan.Password.Equal(state.Password) {
		err := c.ChangeHostingFTPPassword(ctx, state.scope(), state.HostingID.ValueString(), state.ID.ValueString(), plan.Password.ValueString())
		if err != nil {
			common.AddClientErrorWithFields(&resp.Diagnostics, "Could not change the password of FTP account "+state.ID.ValueString(),
				err, map[string]path.Path{"password": path.Root("password")})
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ftpAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ftpAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID, username := state.scope(), state.HostingID.ValueString(), state.ID.ValueString()
	err := c.DeleteHostingFTPAccount(ctx, scope, hostingID, username)
	if err == nil || mailDBGoneWithHosting(ctx, c, scope, hostingID, err) {
		return
	}
	if client.IsCanceled(err) {
		common.AddClientError(&resp.Diagnostics, "Could not delete the FTP account", err)
		return
	}
	hosting, readErr := c.GetWebHosting(ctx, scope, hostingID)
	if readErr != nil {
		common.AddClientError(&resp.Diagnostics, "Could not delete the FTP account", err)
		return
	}
	if account, listErr := findFTPAccount(ctx, c, scope, hosting, username); listErr != nil || account != nil {
		common.AddClientError(&resp.Diagnostics, "Could not delete the FTP account", err)
	}
}

func (r *ftpAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	mailDBImport(ctx, &r.ResourceBase, req, resp, 1, "<username>")
}

// noParentSegment refuses ".." and "." segments, which would leave or alias the directory.
type noParentSegment struct{}

func (noParentSegment) Description(context.Context) string {
	return "value must not contain . or .. segments"
}

func (v noParentSegment) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (noParentSegment) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	for _, segment := range strings.Split(req.ConfigValue.ValueString(), "/") {
		if segment == "." || segment == ".." {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid directory",
				fmt.Sprintf("%q must not contain . or .. segments.", req.ConfigValue.ValueString()))
			return
		}
	}
}
