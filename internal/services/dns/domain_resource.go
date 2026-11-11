package dns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*domainResource)(nil)
	_ resource.ResourceWithImportState = (*domainResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*domainResource)(nil)
)

var domainTimeouts = common.Timeouts{Create: 30 * time.Minute}

// Domain statuses the resource acts on.
const (
	domainPending     = "pending"
	domainRegistering = "registering"
	domainActive      = "active"
	domainFailed      = "failed"
	domainDeleted     = "deleted"
)

// domainGone reports whether a status leaves the name free for a new order: the domain is no
// longer held.
func domainGone(status string) bool {
	return status == domainFailed || status == domainDeleted || status == "pending_delete"
}

// NewDomainResource returns the onetsolutions_domain resource.
func NewDomainResource() resource.Resource {
	return &domainResource{}
}

type domainResource struct {
	common.ResourceBase
}

type domainModel struct {
	ID                 types.String   `tfsdk:"id"`
	OrganizationID     types.String   `tfsdk:"organization_id"`
	ProjectID          types.String   `tfsdk:"project_id"`
	Name               types.String   `tfsdk:"name"`
	DeletionProtection types.Bool     `tfsdk:"deletion_protection"`
	Status             types.String   `tfsdk:"status"`
	RegistrationDate   types.String   `tfsdk:"registration_date"`
	ExpirationDate     types.String   `tfsdk:"expiration_date"`
	AutoRenew          types.Bool     `tfsdk:"auto_renew"`
	RecurringAmount    types.String   `tfsdk:"recurring_amount"`
	NextBillingDate    types.String   `tfsdk:"next_billing_date"`
	DNSZoneID          types.String   `tfsdk:"dns_zone_id"`
	Timeouts           timeouts.Value `tfsdk:"timeouts"`
}

func (m *domainModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (m *domainModel) fill(domain *client.Domain, scope client.Scope) {
	m.ID = types.StringValue(domain.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.Name = types.StringValue(domain.Name)
	m.Status = types.StringValue(domain.Status)
	m.RegistrationDate = types.StringPointerValue(domain.RegistrationDate)
	m.ExpirationDate = types.StringPointerValue(domain.ExpirationDate)
	m.AutoRenew = types.BoolValue(domain.AutoRenew)
	m.RecurringAmount = types.StringPointerValue(domain.RecurringAmount)
	m.NextBillingDate = types.StringPointerValue(domain.NextBillingDate)
	m.DNSZoneID = types.StringNull()
	if domain.DNSZone != nil {
		m.DNSZoneID = types.StringValue(domain.DNSZone.ID)
	}
	if m.DeletionProtection.IsNull() || m.DeletionProtection.IsUnknown() {
		m.DeletionProtection = types.BoolValue(false)
	}
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: description,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Registers a domain name for one year, a billed order that cannot be undone.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("domain"),
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The domain name to register, lowercase without a trailing dot, such as `example.com`. " +
					"Changing it registers the new name, and the previous domain stays registered.",
				Validators:    []validator.String{domainNameValidator{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"deletion_protection": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying (or replacing) the resource is refused. Defaults to `false`. " +
					"Set it to `false` and apply before destroying a protected domain.",
			},
			"status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The registration status: `pending` (ordered, not yet registered), `registering`, " +
					"`active`, `expiring_soon`, `expired`, `suspended`, `transferring_out` or `failed`.",
			},
			"registration_date": computed("When the domain was registered (YYYY-MM-DD), null until it is."),
			"expiration_date":   computed("When the registration expires (YYYY-MM-DD), null until the domain is registered."),
			"auto_renew": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether the domain renews automatically before it expires. Managed by " +
					"`onetsolutions_domain_settings`.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"recurring_amount":  computed("The amount billed at each renewal, in EUR, as a decimal string."),
			"next_billing_date": computed("The next renewal billing date (YYYY-MM-DD), null until the domain is registered."),
			"dns_zone_id":       computed("The UUID of the DNS zone attached to the domain, null when there is none."),
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, domainTimeouts),
		},
	}
}

// domainDestroyNote says what a destroy does: nothing to the domain.
func domainDestroyNote(state *domainModel) string {
	until := "it expires"
	if common.Known(state.ExpirationDate) && state.ExpirationDate.ValueString() != "" {
		until = "it expires on " + state.ExpirationDate.ValueString()
	}
	return fmt.Sprintf("A domain registration cannot be cancelled or refunded: destroying the resource only removes "+
		"domain %s (%s) from the Terraform state. The domain stays registered, with its name servers and DNS zone, "+
		"until %s, and it renews, billed, when automatic renewal is on: turn it off with auto_renew = false in "+
		"onetsolutions_domain_settings to let it expire. Import it again to manage it.",
		state.Name.ValueString(), state.ID.ValueString(), until)
}

// ModifyPlan refuses at plan time a name that cannot be registered, and warns with the price of
// the order a create places, and with what a destroy does.
func (r *domainResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var state *domainModel
	if !req.State.Raw.IsNull() {
		state = &domainModel{}
		resp.Diagnostics.Append(req.State.Get(ctx, state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if req.Plan.Raw.IsNull() {
		if !state.DeletionProtection.ValueBool() {
			resp.Diagnostics.AddWarning("The domain stays registered", domainDestroyNote(state))
		}
		return
	}
	var plan domainModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state != nil && plan.Name.Equal(state.Name) {
		if state.Status.ValueString() == domainFailed {
			resp.Diagnostics.AddWarning("The registration of the domain failed",
				fmt.Sprintf("Domain %s was ordered but not registered. To order it again, replace the resource "+
					"(terraform apply -replace on its address); to stop managing it, remove it from the configuration.",
					state.Name.ValueString()))
		}
		return
	}
	// A replacement is planned again as a create, with no prior state: that plan checks the name.
	if state != nil {
		if !state.DeletionProtection.ValueBool() {
			resp.Diagnostics.AddWarning("Replacing this domain keeps the old one registered", domainDestroyNote(state))
		}
		return
	}
	if !common.Known(plan.Name, plan.OrganizationID, plan.ProjectID) || !r.Data().Ready() {
		return
	}
	r.planOrder(ctx, plan.scope(), plan.Name.ValueString(), &resp.Diagnostics)
}

// planOrder checks that name can be registered in scope and warns with the price. An API failure
// never blocks the plan: the order checks the name again.
func (r *domainResource) planOrder(ctx context.Context, scope client.Scope, name string, diags *diag.Diagnostics) {
	c := r.Data().Client
	nameAttr := path.Root("name")
	if domains, err := c.ListDomains(ctx, scope); err == nil {
		for _, domain := range domains {
			if domain.Name == name && domain.IsActive && !domainGone(domain.Status) {
				diags.AddAttributeError(nameAttr, "The domain is already in the project",
					fmt.Sprintf("Domain %s is already in this project (status %s), as domain %s: import it instead of "+
						"ordering it again, with `terraform import <address> %s` or an import block.",
						name, domain.Status, domain.ID, domain.ID))
				return
			}
		}
	}

	availability, err := c.CheckDomainAvailability(ctx, name)
	if err != nil {
		diags.AddWarning("Could not check the domain",
			"The plan registers a billed domain, but whether "+name+" is available and its price could not be "+
				"checked: "+common.ErrorDetail(err)+". The order checks availability again.")
		return
	}
	if !availability.Available {
		diags.AddAttributeError(nameAttr, "Domain not available",
			fmt.Sprintf("%s cannot be registered: it is already registered, or reserved.%s", name,
				suggestionsSentence(availability.Suggestions)))
		return
	}
	tlds, err := c.ListTLDs(ctx, client.TLDFilter{TLD: availability.TLD})
	if err == nil && !registrable(tlds, availability.TLD) {
		diags.AddAttributeError(nameAttr, "Extension not on sale",
			fmt.Sprintf("Domains in .%s cannot be registered: see onetsolutions_tlds for the extensions on sale.",
				availability.TLD))
		return
	}

	price := "at the price of its extension"
	if availability.Price != nil {
		price = "for " + *availability.Price + " EUR the first year"
	}
	diags.AddWarning("This plan registers a billed domain",
		fmt.Sprintf("Applying it orders the registration of %s for one year, %s, paid from the account balance; "+
			"when the balance does not cover it, the domain is registered once its invoice is paid. A registration "+
			"cannot be cancelled or refunded: destroying the resource later only removes it from the Terraform "+
			"state, and the domain stays registered until it expires.", name, price))
}

func registrable(tlds []client.TLD, tld string) bool {
	for i := range tlds {
		if strings.EqualFold(tlds[i].TLD, tld) {
			return tlds[i].IsActive && tlds[i].IsRegistrationAvailable
		}
	}
	return false
}

func suggestionsSentence(suggestions []client.DomainSuggestion) string {
	names := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		if !s.Available {
			continue
		}
		entry := s.DomainName
		if s.Price != nil {
			entry += " (" + *s.Price + " EUR)"
		}
		names = append(names, entry)
	}
	if len(names) == 0 {
		return ""
	}
	return " Available alternatives: " + strings.Join(names, ", ") + "."
}

// orderRefused reports whether a failed order request was certainly not placed: the API
// answered a client error. A transport error, a client timeout or a server error may come after
// the order was committed.
func orderRefused(err error) bool {
	apiErr, ok := client.AsAPIError(err)
	return ok && apiErr.StatusCode < http.StatusInternalServerError && apiErr.StatusCode != http.StatusRequestTimeout
}

func orderError(err error, name string, diags *diag.Diagnostics) {
	nameAttr := path.Root("name")
	if apiErr, ok := client.AsAPIError(err); ok && apiErr.StatusCode == http.StatusConflict {
		if strings.Contains(apiErr.Detail, "awaiting payment") {
			diags.AddAttributeError(nameAttr, "The domain is already ordered",
				fmt.Sprintf("%s is already on an order awaiting payment. Pay its invoice in the console; the domain is "+
					"then registered: import it rather than ordering it again.", name))
			return
		}
		diags.AddAttributeError(nameAttr, "Domain not available",
			fmt.Sprintf("%s cannot be registered: it is already registered, or reserved. Nothing was ordered.", name))
		return
	}
	if orderRefused(err) {
		common.AddClientErrorWithFields(diags, "Could not order the domain", err,
			map[string]path.Path{"domain_name": nameAttr})
		return
	}
	diags.AddError("The domain may have been ordered", fmt.Sprintf(
		"The order request failed after it was sent, so the order may have been placed: %s\n\nNothing was added to "+
			"the Terraform state. Run terraform plan before applying again: if %s was ordered, the plan says it is "+
			"already in the project and how to import it, so it cannot be ordered twice.",
		common.ErrorDetail(err), name))
}

func awaitingDomainPayment(order *client.DomainOrder, name string) string {
	amount := "the amount due"
	if order.AmountDue != nil {
		amount = fmt.Sprintf("%.2f EUR", *order.AmountDue)
	}
	reference := ""
	if order.OrderNumber != nil && *order.OrderNumber != "" {
		reference = " (order " + *order.OrderNumber + ")"
	}
	pay := "Pay its invoice in the console"
	if order.InvoicePaymentURL != nil && *order.InvoicePaymentURL != "" {
		pay += " (" + *order.InvoicePaymentURL + ")"
	}
	return fmt.Sprintf("%s was ordered%s, but the account balance did not cover it: %s is due. %s: the domain is "+
		"registered once the invoice is paid. It is in the Terraform state with status pending, and the next refresh "+
		"reads its registration.", name, reference, amount, pay)
}

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, domainTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	order, err := c.OrderDomainRegistration(ctx, scope, name)
	if err != nil {
		orderError(err, name, &resp.Diagnostics)
		return
	}
	resp.Diagnostics.Append(common.SavePartialState(ctx, &resp.State, order.DomainID, scope, map[string]attr.Value{
		"name": plan.Name, "deletion_protection": plan.DeletionProtection,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The order is placed: from here on, a failure to follow the registration is a warning, never
	// an error, which would mark the domain tainted and order it again on the next apply.
	if order.AwaitsPayment() {
		resp.Diagnostics.AddWarning("The domain order awaits payment", awaitingDomainPayment(order, name))
		r.saveCurrent(ctx, c, &plan, scope, order.DomainID, &resp.Diagnostics, &resp.State)
		return
	}
	domain, err := r.waitRegistered(ctx, c, scope, order.DomainID)
	var unexpected *waiter.UnexpectedStateError
	switch {
	case err == nil:
		plan.fill(domain, scope)
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	case errors.As(err, &unexpected) && unexpected.State == domainFailed:
		resp.Diagnostics.AddError("The registration of the domain failed",
			fmt.Sprintf("%s was ordered, but its registration failed. The resource stays in the state, marked "+
				"tainted, so the next apply orders it again; to stop, remove it from the configuration.", name))
		if domain != nil {
			plan.fill(domain, scope)
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		}
	default:
		resp.Diagnostics.AddWarning("The domain is still being registered",
			fmt.Sprintf("%s was ordered, and its registration is not complete yet: %s. It is in the Terraform state, "+
				"and the next refresh reads its status.", name, common.ErrorDetail(err)))
		r.saveCurrent(context.WithoutCancel(ctx), c, &plan, scope, order.DomainID, &resp.Diagnostics, &resp.State)
	}
}

// saveCurrent reads the domain into the state; when it cannot be read, the partial state stays.
func (r *domainResource) saveCurrent(ctx context.Context, c *client.Client, plan *domainModel, scope client.Scope, id string, diags *diag.Diagnostics, state interface {
	Set(context.Context, any) diag.Diagnostics
}) {
	domain, err := c.GetDomain(ctx, scope, id)
	if err != nil {
		domain = &client.Domain{ID: id, Name: plan.Name.ValueString(), Status: domainPending, IsActive: true}
	}
	plan.fill(domain, scope)
	diags.Append(state.Set(ctx, plan)...)
}

// waitRegistered waits for an ordered domain to be registered. A failed registration ends the
// wait with an *waiter.UnexpectedStateError and the domain.
func (r *domainResource) waitRegistered(ctx context.Context, c *client.Client, scope client.Scope, id string) (*client.Domain, error) {
	return waiter.Wait[*client.Domain]{
		Description: "domain " + id + " to be registered",
		Pending:     []string{domainPending, domainRegistering},
		Target:      []string{domainActive},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.Domain, string, error) {
			domain, err := c.GetDomain(ctx, scope, id)
			if err != nil {
				return nil, "", err
			}
			return domain, domain.Status, nil
		},
	}.Run(ctx)
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, err := c.GetDomain(ctx, state.scope(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the domain", err)
		return
	}
	if !domain.IsActive || domain.Status == domainDeleted {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(domain, state.scope())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update stores deletion_protection and timeouts, the only attributes that change in place.
func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.DeletionProtection = plan.DeletionProtection
	state.Timeouts = plan.Timeouts
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Delete removes the domain from the state: the API cannot cancel a registration.
func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddError("The domain is protected from deletion",
			fmt.Sprintf("Domain %s has deletion_protection = true. Set it to false and apply before destroying or "+
				"replacing it.", state.Name.ValueString()))
		return
	}
	resp.Diagnostics.AddWarning("The domain stays registered", domainDestroyNote(&state))
}

// ImportState takes "<organization_id>/<project_id>/<domain_id>" or "<domain_id>".
func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
}

type domainNameValidator struct{}

func (domainNameValidator) Description(context.Context) string {
	return "value must be a lowercase domain name without a trailing dot"
}

func (v domainNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (domainNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := checkDomainName(req.ConfigValue.ValueString(), 2); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid domain name", fmt.Sprintf("%s.", err))
	}
}
