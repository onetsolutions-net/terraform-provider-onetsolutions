package hosting

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*emailAccountResource)(nil)
	_ resource.ResourceWithImportState = (*emailAccountResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*emailAccountResource)(nil)
)

const defaultEmailQuotaMB = 250

// Addresses are stored lowercase, so the configuration must spell them so.
var emailAddressPattern = regexp.MustCompile(`^[^@\sA-Z]+@[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?\.[a-z0-9-]+$`)

// NewEmailAccountResource returns the onetsolutions_hosting_email_account resource.
func NewEmailAccountResource() resource.Resource {
	return &emailAccountResource{}
}

type emailAccountResource struct {
	common.ResourceBase
}

type emailAccountModel struct {
	ID                 types.String `tfsdk:"id"`
	OrganizationID     types.String `tfsdk:"organization_id"`
	ProjectID          types.String `tfsdk:"project_id"`
	HostingID          types.String `tfsdk:"hosting_id"`
	Email              types.String `tfsdk:"email"`
	Password           types.String `tfsdk:"password"`
	QuotaMB            types.Int64  `tfsdk:"quota_mb"`
	User               types.String `tfsdk:"user"`
	Domain             types.String `tfsdk:"domain"`
	IsMain             types.Bool   `tfsdk:"is_main"`
	DeletionProtection types.Bool   `tfsdk:"deletion_protection"`
}

func (m *emailAccountModel) scope() client.Scope { return mailDBScope(m.OrganizationID, m.ProjectID) }

func (m *emailAccountModel) fill(account *client.HostingEmailAccount) {
	m.ID = types.StringValue(strings.ToLower(account.Email))
	m.User = types.StringValue(account.User)
	m.Domain = types.StringValue(account.Domain)
	m.IsMain = types.BoolValue(account.IsMain)
}

func (r *emailAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_email_account"
}

func (r *emailAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a mailbox of a web hosting.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID:       mailDBIDAttribute("The address of the mailbox, the same as `email`."),
			mailDBHostingIDAttr: mailDBHostingAttribute(),
			"email": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The address of the mailbox, in lowercase, on a domain of the web hosting, such as " +
					"`contact@example.com`. Changing it recreates the mailbox, which deletes its messages.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(254),
					stringvalidator.RegexMatches(emailAddressPattern, "must be a lowercase email address such as contact@example.com"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password": mailDBPasswordAttribute("The password of the mailbox. Can be changed in place; after an import, " +
				"the next apply sets the mailbox's password to the configured one."),
			"quota_mb": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(defaultEmailQuotaMB),
				MarkdownDescription: fmt.Sprintf("The disk quota of the mailbox in MB; `0` means unlimited. Defaults to `%d`, "+
					"except for the main mailbox, whose quota is then left as it is. Can be changed in place.", defaultEmailQuotaMB),
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
			"user": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The login of the mailbox.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The domain of the address.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"is_main": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether this is the web hosting's main mailbox, which belongs to the hosting account: it " +
					"can be imported to manage its password and quota, and destroying it only removes it from the state.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			mailDBProtectionAttr: schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying (or replacing) the mailbox is refused. Defaults to `false`. Set it " +
					"to `false` and apply before destroying a protected mailbox.",
			},
		}),
	}
}

func (r *emailAccountResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() || req.State.Raw.IsNull() {
		return
	}
	var state emailAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		warnMailboxDeleted(&state, "Destroying", &resp.Diagnostics)
		return
	}
	var plan emailAccountModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if emailAccountReplaced(&plan, &state) {
		warnMailboxDeleted(&state, "Replacing", &resp.Diagnostics)
		return
	}
	keepMainMailboxQuota(ctx, req, resp, &state, &plan)
	if state.Password.IsNull() && common.Known(plan.Password) {
		resp.Diagnostics.AddAttributeWarning(path.Root("password"), "The mailbox's password will be set",
			fmt.Sprintf("The password of %s cannot be read back, so this apply sets it to the configured one.", state.ID.ValueString()))
	}
}

// emailAccountReplaced reports whether the plan replaces the mailbox. resp.RequiresReplace cannot
// say: inside a resource's ModifyPlan it never holds the attributes' RequiresReplace modifiers.
func emailAccountReplaced(plan, state *emailAccountModel) bool {
	return !plan.Email.Equal(state.Email) || !plan.HostingID.Equal(state.HostingID) ||
		!plan.OrganizationID.Equal(state.OrganizationID) || !plan.ProjectID.Equal(state.ProjectID)
}

// keepMainMailboxQuota leaves the quota of the main mailbox as it is when quota_mb is not
// configured, instead of planning the default: the main mailbox belongs to the hosting account,
// and its quota is listed as 0.
func keepMainMailboxQuota(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, state, plan *emailAccountModel) {
	if !state.IsMain.ValueBool() || state.QuotaMB.IsNull() || plan.QuotaMB.Equal(state.QuotaMB) {
		return
	}
	var configured types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("quota_mb"), &configured)...)
	if resp.Diagnostics.HasError() || !configured.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("quota_mb"), state.QuotaMB)...)
}

func warnMailboxDeleted(state *emailAccountModel, verb string, diags *diag.Diagnostics) {
	if state.DeletionProtection.ValueBool() {
		return
	}
	if state.IsMain.ValueBool() {
		diags.AddWarning("The main mailbox is not deleted",
			fmt.Sprintf("%s onetsolutions_hosting_email_account %s only removes it from the Terraform state: the main "+
				"mailbox belongs to the web hosting and cannot be deleted.", verb, state.ID.ValueString()))
		return
	}
	diags.AddWarning("This plan deletes a mailbox and its messages",
		fmt.Sprintf("%s onetsolutions_hosting_email_account %s deletes the mailbox and every message in it. This cannot be undone.",
			verb, state.ID.ValueString()))
}

func (r *emailAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan emailAccountModel
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
	hostingID, email := plan.HostingID.ValueString(), plan.Email.ValueString()

	account, err := c.CreateHostingEmailAccount(ctx, scope, hostingID, client.CreateHostingEmailAccountRequest{
		Email: email, Password: plan.Password.ValueString(), QuotaMB: plan.QuotaMB.ValueInt64(),
	})
	if err != nil && mailDBMayHaveSucceeded(err) {
		account, _ = findEmailAccount(ctx, c, scope, hostingID, email)
	}
	if account == nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the mailbox", err, map[string]path.Path{
			"email": path.Root("email"), "password": path.Root("password"), "quota_mb": path.Root("quota_mb"),
		})
		return
	}
	plan.fill(account)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// findEmailAccount returns the mailbox of that address, nil when there is none.
func findEmailAccount(ctx context.Context, c *client.Client, scope client.Scope, hostingID, email string) (*client.HostingEmailAccount, error) {
	accounts, err := c.ListHostingEmailAccounts(ctx, scope, hostingID)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if strings.EqualFold(accounts[i].Email, email) {
			return &accounts[i], nil
		}
	}
	return nil, nil
}

func (r *emailAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state emailAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID := state.scope(), state.HostingID.ValueString()
	if _, ok := mailDBReadChild(ctx, c, scope, hostingID, resp); !ok {
		return
	}
	account, err := findEmailAccount(ctx, c, scope, hostingID, state.ID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the mailboxes", err)
		return
	}
	if account == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(account)
	state.QuotaMB = types.Int64Value(account.QuotaMB)
	if state.Email.IsNull() {
		state.Email = state.ID
	}
	if state.DeletionProtection.IsNull() {
		state.DeletionProtection = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *emailAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state emailAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID, email := state.scope(), state.HostingID.ValueString(), state.ID.ValueString()
	plan.ID, plan.User, plan.Domain, plan.IsMain = state.ID, state.User, state.Domain, state.IsMain
	plan.OrganizationID, plan.ProjectID = state.OrganizationID, state.ProjectID

	if !plan.Password.Equal(state.Password) {
		if err := c.ChangeHostingEmailPassword(ctx, scope, hostingID, email, plan.Password.ValueString()); err != nil {
			common.AddClientErrorWithFields(&resp.Diagnostics, "Could not change the password of "+email, err, map[string]path.Path{
				"password": path.Root("password"),
			})
			return
		}
		state.Password = plan.Password
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if !plan.QuotaMB.Equal(state.QuotaMB) {
		if err := c.SetHostingEmailQuota(ctx, scope, hostingID, email, plan.QuotaMB.ValueInt64()); err != nil {
			common.AddClientErrorWithFields(&resp.Diagnostics, "Could not set the quota of "+email, err, map[string]path.Path{
				"quota_mb": path.Root("quota_mb"),
			})
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *emailAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state emailAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	email := state.ID.ValueString()
	if state.DeletionProtection.ValueBool() {
		mailDBDeletionProtectionError(&resp.Diagnostics, "mailbox", email)
		return
	}
	if state.IsMain.ValueBool() {
		resp.Diagnostics.AddWarning("The main mailbox was not deleted",
			fmt.Sprintf("%s is the main mailbox of the web hosting: it was only removed from the Terraform state.", email))
		return
	}
	scope, hostingID := state.scope(), state.HostingID.ValueString()
	err := c.DeleteHostingEmailAccount(ctx, scope, hostingID, email)
	if err == nil || mailDBGoneWithHosting(ctx, c, scope, hostingID, err) {
		return
	}
	if client.IsCanceled(err) {
		common.AddClientError(&resp.Diagnostics, "Could not delete the mailbox", err)
		return
	}
	if account, listErr := findEmailAccount(ctx, c, scope, hostingID, email); listErr != nil || account != nil {
		common.AddClientError(&resp.Diagnostics, "Could not delete the mailbox", err)
	}
}

func (r *emailAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	mailDBImport(ctx, &r.ResourceBase, req, resp, 1, "<email>")
}
