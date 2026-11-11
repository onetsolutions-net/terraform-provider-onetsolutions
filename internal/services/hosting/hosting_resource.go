package hosting

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*webHostingResource)(nil)
	_ resource.ResourceWithImportState = (*webHostingResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*webHostingResource)(nil)
)

var webHostingTimeouts = common.Timeouts{Create: 30 * time.Minute, Update: 30 * time.Minute, Delete: 10 * time.Minute}

const webHostingCreateOnlyEffect = "It applies to a new web hosting only."

// NewWebHostingResource returns the onetsolutions_web_hosting resource.
func NewWebHostingResource() resource.Resource {
	return &webHostingResource{}
}

type webHostingResource struct {
	common.ResourceBase
}

type webHostingModel struct {
	ID                     types.String   `tfsdk:"id"`
	OrganizationID         types.String   `tfsdk:"organization_id"`
	ProjectID              types.String   `tfsdk:"project_id"`
	Domain                 types.String   `tfsdk:"domain"`
	PlanID                 types.String   `tfsdk:"plan_id"`
	BillingCycle           types.String   `tfsdk:"billing_cycle"`
	DomainSource           types.String   `tfsdk:"domain_source"`
	EPPCode                types.String   `tfsdk:"epp_code"`
	DeletionProtection     types.Bool     `tfsdk:"deletion_protection"`
	CancellationReasonCode types.String   `tfsdk:"cancellation_reason_code"`
	CancellationReason     types.String   `tfsdk:"cancellation_reason"`
	Status                 types.String   `tfsdk:"status"`
	Username               types.String   `tfsdk:"username"`
	PlanName               types.String   `tfsdk:"plan_name"`
	Price                  types.String   `tfsdk:"price"`
	DiskLimitMB            types.Int64    `tfsdk:"disk_limit_mb"`
	BandwidthLimitMB       types.Int64    `tfsdk:"bandwidth_limit_mb"`
	DedicatedIP            types.String   `tfsdk:"dedicated_ip"`
	Nameservers            types.List     `tfsdk:"nameservers"`
	NextBillingDate        types.String   `tfsdk:"next_billing_date"`
	CancellationRequested  types.String   `tfsdk:"cancellation_requested_at"`
	Created                types.String   `tfsdk:"created"`
	Timeouts               timeouts.Value `tfsdk:"timeouts"`
}

func (m *webHostingModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

// fill copies what the API returns into the model. The order inputs the API does not return keep
// their value; the inputs with a default are filled only when null, right after an import.
func (m *webHostingModel) fill(ctx context.Context, hosting *client.WebHosting, scope client.Scope) diag.Diagnostics {
	m.ID = types.StringValue(hosting.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.Domain = types.StringValue(hosting.Domain)
	m.PlanID = types.StringNull()
	m.PlanName = types.StringNull()
	if hosting.Plan != nil {
		m.PlanID = types.StringValue(hosting.Plan.ID)
		m.PlanName = types.StringValue(hosting.Plan.Name)
	}
	m.BillingCycle = types.StringValue(hosting.BillingCycle)
	if m.DeletionProtection.IsNull() || m.DeletionProtection.IsUnknown() {
		m.DeletionProtection = types.BoolValue(false)
	}
	if m.CancellationReasonCode.IsNull() || m.CancellationReasonCode.IsUnknown() {
		m.CancellationReasonCode = types.StringValue(client.DefaultCancellationReasonCode)
	}
	m.Status = types.StringValue(hosting.Status)
	m.Username = types.StringValue(hosting.Username)
	m.Price = types.StringValue(hosting.Price)
	m.DiskLimitMB = types.Int64Value(hosting.DiskLimitMB)
	m.BandwidthLimitMB = types.Int64Value(hosting.BandwidthLimitMB)
	m.DedicatedIP = nullableString(hosting.DedicatedIP)
	nameservers := []string{}
	for _, ns := range []string{hosting.NS1, hosting.NS2} {
		if ns != "" {
			nameservers = append(nameservers, ns)
		}
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, nameservers)
	m.Nameservers = list
	m.NextBillingDate = nullableString(hosting.NextBillingDate)
	m.CancellationRequested = nullableString(hosting.CancellationRequestedAt)
	m.Created = types.StringValue(hosting.Created)
	return diags
}

// clearUnknown nulls the computed values still unknown, so a partial state can be saved.
func (m *webHostingModel) clearUnknown() {
	for _, s := range []*types.String{&m.Status, &m.Username, &m.PlanName, &m.Price, &m.DedicatedIP,
		&m.NextBillingDate, &m.CancellationRequested, &m.Created, &m.BillingCycle} {
		if s.IsUnknown() {
			*s = types.StringNull()
		}
	}
	for _, n := range []*types.Int64{&m.DiskLimitMB, &m.BandwidthLimitMB} {
		if n.IsUnknown() {
			*n = types.Int64Null()
		}
	}
	if m.Nameservers.IsUnknown() {
		m.Nameservers = types.ListNull(types.StringType)
	}
}

func (r *webHostingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_web_hosting"
}

func (r *webHostingResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	createOnly := []planmodifier.String{common.CreateOnlyWarn(webHostingCreateOnlyEffect)}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an OnetSolutions web hosting, a billed order on its billing cycle.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("web hosting"),
			"domain": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The primary domain of the web hosting, such as `example.com`: lowercase, without " +
					"a trailing dot. Changing it replaces the web hosting.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(253),
					stringvalidator.RegexMatches(webHostingDomainPattern, webHostingDomainMessage),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"plan_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The UUID of the plan (see `onetsolutions_hosting_plans`). Can be changed in place to " +
					"a plan the web hosting is eligible to (checked at plan time): the change is an upgrade order, applied " +
					"once paid.",
				Validators: []validator.String{common.UUID()},
			},
			"billing_cycle": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The billing cycle, one of `" + strings.Join(webHostingBillingCycles, "`, `") + "`. " +
					"When unset, `" + webHostingDefaultCycle + "` for a new web hosting, and the current cycle afterwards. " +
					"`free` and `one_time` exist only on web hostings already billed that way, such as imported ones. The " +
					"cycle changes only together with a new `plan_id`, and only to a longer cycle.",
				Validators:    []validator.String{stringvalidator.OneOf(webHostingBillingCycles...)},
				PlanModifiers: keep,
			},
			"domain_source": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Where `domain` comes from, applied by the order only: `" + sourceExternal + "` (a " +
					"domain registered elsewhere, pointed at the web hosting), `" + sourceExisting + "` (a domain of the " +
					"organization), `" + sourceRegister + "` (registers the domain, billed on the same order) or `" +
					sourceTransfer + "` (transfers it in with `epp_code`, billed on the same order). When unset, `" +
					sourceExternal + "`. A later change is stored with a plan-time warning and has no effect.",
				Validators:    []validator.String{stringvalidator.OneOf(webHostingDomainSources...)},
				PlanModifiers: createOnly,
			},
			"epp_code": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "The transfer code of the domain, required with `domain_source = \"" + sourceTransfer +
					"\"` and refused otherwise. Sent by the order only.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 255)},
				PlanModifiers: createOnly,
			},
			"deletion_protection": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying (or replacing) the web hosting is refused. Defaults to `false`. " +
					"Set it to `false` and apply before destroying a protected web hosting.",
			},
			"cancellation_reason_code": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(client.DefaultCancellationReasonCode),
				MarkdownDescription: "Why the web hosting is cancelled when the resource is destroyed or replaced, one of `" +
					strings.Join(client.CancellationReasonCodes, "`, `") + "`. Defaults to `" +
					client.DefaultCancellationReasonCode + "`. Sent only by the destroy: a change is stored without any " +
					"request, so set it and apply before destroying.",
				Validators: []validator.String{stringvalidator.OneOf(client.CancellationReasonCodes...)},
			},
			"cancellation_reason": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: fmt.Sprintf("A sentence of at least %d words explaining the cancellation, sent "+
					"with `cancellation_reason_code` when the resource is destroyed or replaced. When unset, a default "+
					"sentence is recorded. Sent only by the destroy: a change is stored without any request.",
					client.CancellationReasonMinWords),
				Validators: []validator.String{
					stringvalidator.LengthAtMost(client.CancellationReasonMaxLength),
					common.NoSurroundingWhitespace(),
					minReasonWords(client.CancellationReasonMinWords),
				},
			},
			"status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "`pending` (ordered, being set up or awaiting payment), `active`, `suspended`, " +
					"`terminated`, `cancelled` or `failed`.",
			},
			"username": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The account's login name, empty until the web hosting is set up.",
			},
			"plan_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The name of the plan.",
			},
			"price": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The price per billing cycle, in EUR excluding tax, as a decimal string.",
			},
			"disk_limit_mb": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The disk space allowed, in MB.",
			},
			"bandwidth_limit_mb": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The monthly bandwidth allowed, in MB; 0 means unlimited.",
			},
			"dedicated_ip": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The dedicated IP address, null when none is assigned.",
			},
			"nameservers": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The name servers to delegate the domain to.",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"next_billing_date": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The next renewal date (RFC 3339), null when none is scheduled.",
			},
			"cancellation_requested_at": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "When a cancellation at the end of the billing period was requested (RFC 3339), " +
					"null when none is. Set by a destroy, or by a cancellation requested in the console: the web hosting " +
					"then stays in the state, unchanged, until it ends.",
				PlanModifiers: keep,
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the web hosting was created (RFC 3339).",
				PlanModifiers:       keep,
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, webHostingTimeouts),
		},
	}
}

func (m *webHostingModel) orderRequest() client.HostingOrderCreate {
	in := client.HostingOrderCreate{
		PlanID:       m.PlanID.ValueString(),
		DomainSource: m.DomainSource.ValueString(),
		DomainName:   m.Domain.ValueString(),
		EPPCode:      m.EPPCode.ValueString(),
		BillingCycle: m.BillingCycle.ValueString(),
	}
	if in.DomainSource == "" {
		in.DomainSource = sourceExternal
	}
	if in.BillingCycle == "" {
		in.BillingCycle = webHostingDefaultCycle
	}
	return in
}

// orderFields maps the API's field errors on an order to attributes.
var orderFields = map[string]path.Path{
	"plan_id": path.Root("plan_id"), "domain_source": path.Root("domain_source"), "domain_name": path.Root("domain"),
	"epp_code": path.Root("epp_code"), "billing_cycle": path.Root("billing_cycle"),
}

func (r *webHostingResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		warnWebHostingDestroy(ctx, req, &resp.Diagnostics)
		return
	}
	var plan webHostingModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.State.Raw.IsNull() {
		checkWebHostingOrderable(&plan, &resp.Diagnostics)
		if !resp.Diagnostics.HasError() && r.Data().Ready() {
			r.previewOrder(ctx, &plan, &resp.Diagnostics)
		}
		return
	}
	var state webHostingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	warnWebHostingReplacement(&plan, &state, &resp.Diagnostics)
	if !common.Known(plan.PlanID, plan.BillingCycle) || (plan.PlanID.Equal(state.PlanID) && plan.BillingCycle.Equal(state.BillingCycle)) {
		return
	}
	if !checkPlanChange(&plan, &state, &resp.Diagnostics) || !r.Data().Ready() {
		return
	}
	r.planChange(ctx, &plan, &state, &resp.Diagnostics)
}

// webHostingCancellationEffect says what destroying a web hosting does to its billing.
func webHostingCancellationEffect(state *webHostingModel) string {
	until := "the end of its billing period"
	if common.Known(state.NextBillingDate) && state.NextBillingDate.ValueString() != "" {
		until = state.NextBillingDate.ValueString()
	}
	if common.Known(state.CancellationRequested) {
		return "Its cancellation is already requested: the destroy only removes it from the Terraform state, and it " +
			"keeps running and billing until " + until + "."
	}
	return "Destroying it does not delete it: it requests its cancellation at the end of the billing period, and the " +
		"web hosting keeps running and billing until " + until + ", then is closed with its files, databases and " +
		"e-mail. Revoking the cancellation in the console before then keeps it. A domain registered or transferred " +
		"with it is not cancelled."
}

// warnWebHostingDestroy says at plan time what a destroy costs. A protected web hosting gets no
// warning: its destroy fails.
func warnWebHostingDestroy(ctx context.Context, req resource.ModifyPlanRequest, diags *diag.Diagnostics) {
	if req.State.Raw.IsNull() {
		return
	}
	var state webHostingModel
	if d := req.State.Get(ctx, &state); d.HasError() || state.DeletionProtection.ValueBool() {
		return
	}
	diags.AddWarning("This plan cancels a billed web hosting",
		fmt.Sprintf("Web hosting %s (%s) leaves the Terraform state. %s", state.ID.ValueString(), state.Domain.ValueString(),
			webHostingCancellationEffect(&state)))
}

// warnWebHostingReplacement says that a new domain orders a new web hosting while the old one
// stays billed until the end of its period.
func warnWebHostingReplacement(plan, state *webHostingModel, diags *diag.Diagnostics) {
	if !common.Known(plan.Domain) || plan.Domain.Equal(state.Domain) || state.DeletionProtection.ValueBool() {
		return
	}
	diags.AddWarning("Replacing this web hosting keeps the old one billed",
		fmt.Sprintf("The plan replaces web hosting %s (%s): it orders a new, billed web hosting for %s, and the old one "+
			"is not deleted. %s", state.ID.ValueString(), state.Domain.ValueString(), plan.Domain.ValueString(),
			webHostingCancellationEffect(state)))
}

// checkWebHostingOrderable refuses at plan time what the order refuses.
func checkWebHostingOrderable(plan *webHostingModel, diags *diag.Diagnostics) {
	if cycle := plan.BillingCycle.ValueString(); common.Known(plan.BillingCycle) && webHostingCycleRank(cycle) == 0 {
		diags.AddAttributeError(path.Root("billing_cycle"), "Billing cycle not orderable",
			fmt.Sprintf("A web hosting cannot be ordered on the %s billing cycle: choose monthly or a longer cycle.", cycle))
	}
	if !common.Known(plan.DomainSource) && !plan.DomainSource.IsNull() {
		return
	}
	transfer := plan.DomainSource.ValueString() == sourceTransfer
	switch {
	case transfer && plan.EPPCode.IsNull():
		diags.AddAttributeError(path.Root("epp_code"), "epp_code is required for a transfer",
			"A domain transferred in with the web hosting needs its transfer code: set epp_code.")
	case !transfer && !plan.EPPCode.IsNull():
		diags.AddAttributeError(path.Root("epp_code"), "epp_code is only for a transfer",
			"epp_code is sent only when domain_source is \"transfer\": remove it.")
	}
}

// checkPlanChange refuses at plan time a change of the billing cycle without a new plan, or to a
// shorter cycle: the API changes the cycle only with a new plan, and only to a longer one.
func checkPlanChange(plan, state *webHostingModel, diags *diag.Diagnostics) bool {
	if plan.PlanID.Equal(state.PlanID) {
		diags.AddAttributeError(path.Root("billing_cycle"), "The billing cycle cannot change alone",
			fmt.Sprintf("The billing cycle of a web hosting changes only with a new plan. Set billing_cycle back to %q, "+
				"change it together with plan_id, or change it in the console; the next refresh then reads the new cycle.",
				state.BillingCycle.ValueString()))
		return false
	}
	if !plan.BillingCycle.Equal(state.BillingCycle) && webHostingCycleRank(plan.BillingCycle.ValueString()) < webHostingCycleRank(state.BillingCycle.ValueString()) {
		diags.AddAttributeError(path.Root("billing_cycle"), "The billing cycle can only get longer",
			fmt.Sprintf("A plan change keeps the billing cycle %q or moves to a longer one, not to %q.",
				state.BillingCycle.ValueString(), plan.BillingCycle.ValueString()))
		return false
	}
	return true
}

// previewOrder checks the plan and the billing cycle against the plans on sale and warns with
// the price of the order a create places. A failed listing never blocks the plan.
func (r *webHostingResource) previewOrder(ctx context.Context, plan *webHostingModel, diags *diag.Diagnostics) {
	if !common.Known(plan.PlanID) {
		return
	}
	cycle := webHostingDefaultCycle
	if common.Known(plan.BillingCycle) {
		cycle = plan.BillingCycle.ValueString()
	}
	plans, err := r.Data().Client.ListHostingPlans(ctx)
	if err != nil {
		tflog.Warn(ctx, "hosting plans listing failed", map[string]any{"error": err.Error()})
		diags.AddWarning("Could not price the web hosting order",
			"The plan orders a billed web hosting, but its price could not be read: "+common.ErrorDetail(err))
		return
	}
	i := slices.IndexFunc(plans, func(p client.HostingPlan) bool { return p.ID == plan.PlanID.ValueString() && p.IsActive })
	if i < 0 {
		diags.AddAttributeError(path.Root("plan_id"), "Plan not on sale",
			fmt.Sprintf("No web hosting plan %s is on sale. Plans on sale: %s.", plan.PlanID.ValueString(), hostingPlanNames(activePlans(plans))))
		return
	}
	price, ok := hostingPlanPrice(&plans[i], cycle)
	if !ok {
		diags.AddAttributeError(path.Root("billing_cycle"), "Billing cycle not offered",
			fmt.Sprintf("Plan %s is not sold on the %s billing cycle. Its cycles: %s.", plans[i].Name, cycle, hostingPlanCycles(&plans[i])))
		return
	}
	domain := "the domain"
	if common.Known(plan.Domain) {
		domain = plan.Domain.ValueString()
	}
	detail := fmt.Sprintf("Applying it orders a web hosting for %s on plan %s, billed %s EUR excluding tax on the %s "+
		"billing cycle. Destroying it later requests its cancellation at the end of the billing period: it is billed "+
		"until then. When the account balance does not cover the order, the web hosting waits for its invoice to be paid.",
		domain, plans[i].Name, price, cycle)
	switch plan.DomainSource.ValueString() {
	case sourceRegister:
		detail += " The order also registers the domain, billed on the same order; registering a domain cannot be undone."
	case sourceTransfer:
		detail += " The order also transfers the domain in, billed on the same order."
	}
	diags.AddWarning("This plan orders a billed web hosting", detail)
}

func activePlans(plans []client.HostingPlan) []client.HostingPlan {
	out := make([]client.HostingPlan, 0, len(plans))
	for i := range plans {
		if plans[i].IsActive {
			out = append(out, plans[i])
		}
	}
	return out
}

// planChange refuses at plan time a plan the web hosting is not eligible to, and warns with the
// recurring price of the new plan. A failed listing never blocks the plan.
func (r *webHostingResource) planChange(ctx context.Context, plan, state *webHostingModel, diags *diag.Diagnostics) {
	eligible, err := r.Data().Client.ListEligibleHostingPlans(ctx, state.scope(), state.ID.ValueString())
	if err != nil {
		diags.AddWarning("Could not check the new plan",
			"The plans this web hosting can change to could not be listed: "+common.ErrorDetail(err))
		return
	}
	i := slices.IndexFunc(eligible, func(p client.HostingPlan) bool { return p.ID == plan.PlanID.ValueString() })
	if i < 0 {
		diags.AddAttributeError(path.Root("plan_id"), "Plan not eligible",
			fmt.Sprintf("Web hosting %s cannot change to plan %s. Eligible plans: %s.", state.ID.ValueString(),
				plan.PlanID.ValueString(), hostingPlanNames(eligible)))
		return
	}
	cycle := plan.BillingCycle.ValueString()
	price, ok := hostingPlanPrice(&eligible[i], cycle)
	if !ok {
		diags.AddAttributeError(path.Root("billing_cycle"), "Billing cycle not offered",
			fmt.Sprintf("Plan %s is not sold on the %s billing cycle. Its cycles: %s.", eligible[i].Name, cycle, hostingPlanCycles(&eligible[i])))
		return
	}
	diags.AddWarning("This plan changes the billing of a web hosting",
		fmt.Sprintf("Applying it orders a change of web hosting %s to plan %s, billed %s EUR excluding tax on the %s "+
			"billing cycle, plus the difference for the current period. The change applies once the order is paid.",
			state.ID.ValueString(), eligible[i].Name, price, cycle))
}

// webHostingUntaintHint completes an error raised once the web hosting exists: Terraform marks
// it tainted, and replacing it would cancel a billed web hosting and order another.
const webHostingUntaintHint = "\n\nThe web hosting stays in the state, marked tainted, so the next apply would " +
	"replace it. To keep it, wait until it is active, run `terraform untaint` on its address, then apply again."

func (r *webHostingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webHostingModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, webHostingTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := plan.orderRequest()
	order, err := c.OrderWebHosting(ctx, scope, in)
	if err != nil {
		if webHostingOrderRefused(err) {
			common.AddClientErrorWithFields(&resp.Diagnostics, "Could not order the web hosting", err, orderFields)
			return
		}
		resp.Diagnostics.AddError("The web hosting may have been ordered", fmt.Sprintf(
			"The order request failed after it was sent, so the order may have been placed: %s\n\nNothing was added "+
				"to the Terraform state. Before applying again, check the console: if a web hosting for %s was "+
				"ordered, import it (terraform import), or cancel it; otherwise a second one is ordered.",
			common.ErrorDetail(err), in.DomainName))
		return
	}
	if order.HostingID == "" {
		resp.Diagnostics.AddError("The web hosting was ordered but cannot be found", fmt.Sprintf(
			"The order for %s was placed, but the answer did not name the web hosting it created. Find it in the "+
				"console and import it (terraform import), or cancel it, before applying again, or a second web hosting "+
				"is ordered.", in.DomainName))
		return
	}

	plan.ID = types.StringValue(order.HostingID)
	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	if plan.BillingCycle.IsUnknown() {
		plan.BillingCycle = types.StringValue(in.BillingCycle)
	}
	plan.clearUnknown()
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if order.AwaitsPayment() {
		if hosting, getErr := c.GetWebHosting(ctx, scope, order.HostingID); getErr == nil {
			resp.Diagnostics.Append(plan.fill(ctx, hosting, scope)...)
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		}
		resp.Diagnostics.AddWarning("The web hosting order awaits payment", awaitingPaymentDetail(order))
		return
	}

	hosting, err := r.waitActive(ctx, c, scope, order.HostingID)
	if err != nil {
		resp.Diagnostics.AddError("Could not create the web hosting", common.ErrorDetail(err)+webHostingUntaintHint)
		return
	}
	resp.Diagnostics.Append(plan.fill(ctx, hosting, scope)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// webHostingOrderRefused reports whether a failed order request was certainly not placed: the API
// answered a client error. A transport error, a client timeout or a server error may come after
// the order was committed.
func webHostingOrderRefused(err error) bool {
	apiErr, ok := client.AsAPIError(err)
	return ok && apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError &&
		apiErr.StatusCode != http.StatusRequestTimeout
}

// awaitingPaymentDetail explains an order placed but unpaid: the web hosting is in the state,
// pending, and is set up once the invoice is paid.
func awaitingPaymentDetail(order *client.HostingOrder) string {
	reference := "The order"
	if order.OrderNumber != nil && *order.OrderNumber != "" {
		reference = "Order " + *order.OrderNumber
	}
	if order.InvoiceID != nil {
		reference += fmt.Sprintf(" (invoice %d)", *order.InvoiceID)
	}
	pay := "Pay its invoice in the console"
	if order.InvoicePaymentURL != nil && *order.InvoicePaymentURL != "" {
		pay += " (" + *order.InvoicePaymentURL + ")"
	}
	return fmt.Sprintf("%s for %s was placed, but the account balance did not cover it: %s is due. The web hosting is "+
		"in the Terraform state with status pending, and it is set up once the order is paid. %s; the next refresh "+
		"then reads it active. Until then, its PHP versions and WordPress sites cannot be managed.",
		reference, order.DomainName, formatEUR(order.AmountDue), pay)
}

// waitActive waits until the web hosting is set up; failed, terminated or cancelled ends the wait
// with an error.
func (r *webHostingResource) waitActive(ctx context.Context, c *client.Client, scope client.Scope, id string) (*client.WebHosting, error) {
	return waiter.Wait[*client.WebHosting]{
		Description: "web hosting " + id + " to become active",
		Pending:     []string{client.WebHostingStatusPending},
		Target:      []string{client.WebHostingStatusActive},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.WebHosting, string, error) {
			hosting, err := c.GetWebHosting(ctx, scope, id)
			if err != nil {
				return nil, "", err
			}
			return hosting, hosting.Status, nil
		},
	}.Run(ctx)
}

func (r *webHostingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webHostingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	hosting, err := c.GetWebHosting(ctx, state.scope(), state.ID.ValueString())
	if client.IsNotFound(err) || (err == nil && hosting.Gone()) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return
	}
	resp.Diagnostics.Append(state.fill(ctx, hosting, state.scope())...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	if hosting.CancellationRequestedAt != nil {
		until := "the end of its billing period"
		if hosting.NextBillingDate != nil && *hosting.NextBillingDate != "" {
			until = *hosting.NextBillingDate
		}
		resp.Diagnostics.AddWarning("The web hosting's cancellation is requested",
			fmt.Sprintf("Web hosting %s (%s) has a cancellation requested: it runs and is billed until %s, then it is "+
				"closed with its data and the next plan orders a new one. It stays in the Terraform state until then. To "+
				"keep it, revoke the cancellation in the console; to let it end, remove it from the configuration.",
				hosting.ID, hosting.Domain, until))
	}
}

func (r *webHostingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state webHostingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpUpdate, webHostingTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := state.scope()
	id := state.ID.ValueString()
	if err := r.changePlan(ctx, c, &plan, &state); err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not change the plan of the web hosting", err,
			map[string]path.Path{"plan_id": path.Root("plan_id"), "billing_cycle": path.Root("billing_cycle")})
		r.saveAfterFailure(ctx, c, &state, &plan, resp)
		return
	}
	hosting, err := c.GetWebHosting(ctx, scope, id)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(plan.fill(ctx, hosting, scope)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(common.ClearImported(ctx, resp.Private)...)
}

// saveAfterFailure stores what the API now reports, so the plan and billing cycle in the state
// stay those the web hosting has: the inputs the API does not return come from the plan.
func (r *webHostingResource) saveAfterFailure(ctx context.Context, c *client.Client, state, plan *webHostingModel, resp *resource.UpdateResponse) {
	hosting, err := c.GetWebHosting(context.WithoutCancel(ctx), state.scope(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	merged := *plan
	merged.ID = state.ID
	merged.clearUnknown()
	resp.Diagnostics.Append(merged.fill(ctx, hosting, state.scope())...)
	resp.Diagnostics.Append(resp.State.Set(ctx, merged)...)
}

// changePlan orders the new plan, with the new billing cycle when it changes too, and waits until
// the web hosting reports both: the change applies once its order is paid.
func (r *webHostingResource) changePlan(ctx context.Context, c *client.Client, plan, state *webHostingModel) error {
	if plan.PlanID.Equal(state.PlanID) && plan.BillingCycle.Equal(state.BillingCycle) {
		return nil
	}
	scope := state.scope()
	id := state.ID.ValueString()
	if plan.PlanID.Equal(state.PlanID) {
		return fmt.Errorf("the billing cycle of web hosting %s cannot change without a new plan_id", id)
	}
	in := client.HostingPlanChange{PlanID: plan.PlanID.ValueString()}
	if !plan.BillingCycle.Equal(state.BillingCycle) {
		in.BillingCycle = plan.BillingCycle.ValueString()
	}
	order, err := c.ChangeWebHostingPlan(ctx, scope, id, in)
	if err != nil {
		return err
	}
	cycle := plan.BillingCycle.ValueString()
	description := fmt.Sprintf("web hosting %s to have plan %s on the %s billing cycle", id, in.PlanID, cycle)
	if order.InvoiceURL != nil && *order.InvoiceURL != "" {
		description += " (the change applies once its invoice is paid: " + *order.InvoiceURL + ")"
	}
	_, err = waiter.Wait[*client.WebHosting]{
		Description: description,
		Pending:     []string{"ordered"},
		Target:      []string{"applied"},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.WebHosting, string, error) {
			hosting, getErr := c.GetWebHosting(ctx, scope, id)
			if getErr != nil {
				return nil, "", getErr
			}
			if hosting.Plan != nil && hosting.Plan.ID == in.PlanID && hosting.BillingCycle == cycle {
				return hosting, "applied", nil
			}
			return hosting, "ordered", nil
		},
	}.Run(ctx)
	return err
}

// Delete requests the cancellation of the web hosting at the end of its billing period: the API
// has no deletion. A cancellation already requested, or a web hosting that already ended, counts
// as done. Right after an order, the web hosting cannot be cancelled until it is linked to its
// billing service: the request is retried until the timeout.
func (r *webHostingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webHostingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddError("The web hosting is protected from deletion",
			fmt.Sprintf("Web hosting %s has deletion_protection = true. Set it to false and apply before destroying or "+
				"replacing it.", id))
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, webHostingTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.HostingCancellation{ReasonCode: state.CancellationReasonCode.ValueString(), Reason: state.CancellationReason.ValueString()}
	if in.ReasonCode == "" {
		in.ReasonCode = client.DefaultCancellationReasonCode
	}
	if in.Reason == "" {
		in.Reason = client.HostingCancellationDefaultReason
	}
	var hosting *client.WebHosting
	err := waiter.Retry(ctx, r.Data().Waiter, "requesting the cancellation of web hosting "+id, func(ctx context.Context) error {
		var cancelErr error
		hosting, cancelErr = c.CancelWebHosting(ctx, state.scope(), id, in)
		return cancelErr
	}, func(err error) bool { return client.HasCode(err, client.CodeHostingNotLinkedToBilling) })
	switch {
	case client.IsNotFound(err):
		return
	case client.HasCode(err, client.CodeHostingNotCancellable):
		resp.Diagnostics.AddWarning("The web hosting had already ended",
			fmt.Sprintf("Web hosting %s (%s) is terminated or cancelled: nothing was requested.", id, state.Domain.ValueString()))
		return
	case client.HasCode(err, client.CodeHostingCancellationRequested):
		resp.Diagnostics.AddWarning("The web hosting's cancellation was already requested", fmt.Sprintf(
			"Web hosting %s (%s) already had a cancellation requested: it was not changed, and it keeps running and "+
				"billing until the end of its billing period. It is no longer in the Terraform state.", id, state.Domain.ValueString()))
		return
	case client.HasCode(err, client.CodeHostingNotLinkedToBilling):
		resp.Diagnostics.AddError("The web hosting cannot be cancelled yet", fmt.Sprintf(
			"%s\n\nWeb hosting %s is not linked to its billing service yet, which happens shortly after its order is "+
				"placed. It stays in the Terraform state: destroy it again in a few minutes. If its order is unpaid, pay "+
				"or cancel its invoice in the console first.", common.ErrorDetail(err), id))
		return
	case err != nil:
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not request the cancellation of the web hosting", err,
			map[string]path.Path{"reason_code": path.Root("cancellation_reason_code"), "reason": path.Root("cancellation_reason")})
		return
	}
	until := "the end of its billing period"
	if hosting.NextBillingDate != nil && *hosting.NextBillingDate != "" {
		until = *hosting.NextBillingDate
	}
	resp.Diagnostics.AddWarning("The web hosting's cancellation is requested", fmt.Sprintf(
		"Web hosting %s (%s) was not deleted: its cancellation is requested, and it keeps running and billing until %s, "+
			"when it is closed with its files, databases and e-mail. It is no longer in the Terraform state. To keep it, "+
			"revoke the cancellation in the console before that date, then import it again.", hosting.ID, hosting.Domain, until))
}

// ImportState imports a web hosting by "<organization_id>/<project_id>/<id>" or "<id>", and
// marks it imported, so its order inputs are taken from the configuration without a warning.
func (r *webHostingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(common.MarkImported(ctx, resp.Private)...)
	}
}
