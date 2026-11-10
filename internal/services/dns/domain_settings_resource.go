package dns

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*domainSettingsResource)(nil)
	_ resource.ResourceWithImportState = (*domainSettingsResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*domainSettingsResource)(nil)
)

var domainSettingsTimeouts = common.Timeouts{Create: 15 * time.Minute, Update: 15 * time.Minute}

// DomainSettingsDeleteNote explains what destroying the resource does, in the plan warning.
const DomainSettingsDeleteNote = "Destroying this resource only removes it from the Terraform state: the domain keeps " +
	"its name servers, renewal, lock and privacy settings as they are, and stays registered."

// NewDomainSettingsResource returns the onetsolutions_domain_settings resource.
func NewDomainSettingsResource() resource.Resource {
	return &domainSettingsResource{}
}

type domainSettingsResource struct {
	common.ResourceBase
}

type domainSettingsModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	DomainID       types.String   `tfsdk:"domain_id"`
	Name           types.String   `tfsdk:"name"`
	Nameservers    types.List     `tfsdk:"nameservers"`
	AutoRenew      types.Bool     `tfsdk:"auto_renew"`
	Locked         types.Bool     `tfsdk:"locked"`
	WhoisPrivacy   types.Bool     `tfsdk:"whois_privacy"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (m *domainSettingsModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (m *domainSettingsModel) nameservers(ctx context.Context) ([]string, diag.Diagnostics) {
	var values []string
	diags := m.Nameservers.ElementsAs(ctx, &values, false)
	return values, diags
}

func (m *domainSettingsModel) fill(ctx context.Context, domain *client.Domain, scope client.Scope) {
	m.ID = types.StringValue(domain.ID)
	m.DomainID = types.StringValue(domain.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.Name = types.StringValue(domain.Name)
	current := domain.NameserverList()
	held, _ := m.nameservers(ctx)
	if m.Nameservers.IsNull() || m.Nameservers.IsUnknown() || !sameNameservers(held, current) {
		m.Nameservers = stringListValue(ctx, current)
	}
	m.AutoRenew = types.BoolValue(domain.AutoRenew)
	m.Locked = types.BoolValue(domain.IsLocked)
	m.WhoisPrivacy = types.BoolValue(domain.WhoisPrivacy)
}

// sameNameservers compares two delegations the way registries do: in order, ignoring case and
// a trailing dot.
func sameNameservers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(strings.TrimSuffix(a[i], "."), strings.TrimSuffix(b[i], ".")) {
			return false
		}
	}
	return true
}

func (r *domainSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_settings"
}

func (r *domainSettingsResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the name servers, automatic renewal, registry lock and WHOIS privacy of a " +
			"registered domain.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The UUID of the domain.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the domain (see `onetsolutions_domain`). Changing it moves the resource to another domain (the previous domain is left unchanged).",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The domain name.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"nameservers": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The name servers the domain is delegated to, 2 to 5, in order, lowercase without a " +
					"trailing dot — for instance the `nameservers` of an `onetsolutions_dns_zone`.",
				Validators: []validator.List{
					listvalidator.SizeBetween(2, 5),
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(hostNameValidator{}),
				},
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"auto_renew": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the domain renews automatically before it expires.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"locked": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the domain is locked at the registry against transfers.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"whois_privacy": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the registrant's contact details are hidden from WHOIS.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, domainSettingsTimeouts),
		},
	}
}

// ModifyPlan reports, on destroy, that the domain is left as it is.
func (r *domainSettingsResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if req.Plan.Raw.IsNull() && !req.State.Raw.IsNull() {
		resp.Diagnostics.AddWarning("The domain is left unchanged", DomainSettingsDeleteNote)
	}
}

// apply changes every setting of plan that is known and differs from the domain, waiting for
// the task of each, then reads the domain back into plan.
func (r *domainSettingsResource) apply(ctx context.Context, c *client.Client, plan *domainSettingsModel, scope client.Scope, diags *diag.Diagnostics) {
	domainID := plan.DomainID.ValueString()
	domain, err := c.GetDomain(ctx, scope, domainID)
	if err != nil {
		common.AddClientError(diags, "Could not read the domain", err)
		return
	}
	wait := func(what string, task *client.DomainTask, err error) bool {
		if err != nil {
			common.AddClientError(diags, "Could not change the "+what+" of the domain", err)
			return false
		}
		if task == nil {
			return true
		}
		if _, err := waiter.DomainTask(ctx, r.Data().Waiter, c, scope, domainID, task.ID); err != nil {
			common.AddClientError(diags, "Changing the "+what+" of the domain failed", err)
			return false
		}
		return true
	}

	if !plan.Nameservers.IsNull() && !plan.Nameservers.IsUnknown() {
		wanted, d := plan.nameservers(ctx)
		diags.Append(d...)
		if diags.HasError() {
			return
		}
		if !sameNameservers(wanted, domain.NameserverList()) {
			task, updateErr := c.UpdateDomainNameservers(ctx, scope, domainID, wanted)
			if !wait("name servers", task, updateErr) {
				return
			}
		}
	}
	if common.Known(plan.AutoRenew) && plan.AutoRenew.ValueBool() != domain.AutoRenew {
		if _, renewErr := c.SetDomainAutoRenew(ctx, scope, domainID, plan.AutoRenew.ValueBool()); renewErr != nil {
			common.AddClientError(diags, "Could not change the automatic renewal of the domain", renewErr)
			return
		}
	}
	if common.Known(plan.Locked) && plan.Locked.ValueBool() != domain.IsLocked {
		task, lockErr := c.SetDomainLock(ctx, scope, domainID, plan.Locked.ValueBool())
		if !wait("lock", task, lockErr) {
			return
		}
	}
	if common.Known(plan.WhoisPrivacy) && plan.WhoisPrivacy.ValueBool() != domain.WhoisPrivacy {
		task, privacyErr := c.SetDomainWhoisPrivacy(ctx, scope, domainID, plan.WhoisPrivacy.ValueBool())
		if !wait("WHOIS privacy", task, privacyErr) {
			return
		}
	}

	domain, err = c.GetDomain(ctx, scope, domainID)
	if err != nil {
		common.AddClientError(diags, "Could not read the domain", err)
		return
	}
	plan.fill(ctx, domain, scope)
}

func (r *domainSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainSettingsModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, domainSettingsTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, c, &plan, scope, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *domainSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	domainID := state.DomainID.ValueString()
	if domainID == "" {
		domainID = state.ID.ValueString()
	}
	domain, err := c.GetDomain(ctx, state.scope(), domainID)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the domain", err)
		return
	}
	if !domain.IsActive || domain.Status == "deleted" {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(ctx, domain, state.scope())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *domainSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpUpdate, domainSettingsTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, c, &plan, state.scope(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete forgets the resource and leaves the domain as it is.
func (r *domainSettingsResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

// ImportState takes "<organization_id>/<project_id>/<domain_id>" or "<domain_id>".
func (r *domainSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var id types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain_id"), id)...)
}

type hostNameValidator struct{}

func (hostNameValidator) Description(context.Context) string {
	return "value must be a lowercase host name without a trailing dot"
}

func (v hostNameValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (hostNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := checkDomainName(req.ConfigValue.ValueString(), 2); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid name server", fmt.Sprintf("%s.", err))
	}
}
