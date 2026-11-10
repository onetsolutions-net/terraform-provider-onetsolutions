package network

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure        = (*instanceIPReverseDNSResource)(nil)
	_ resource.ResourceWithImportState      = (*instanceIPReverseDNSResource)(nil)
	_ resource.ResourceWithModifyPlan       = (*instanceIPReverseDNSResource)(nil)
	_ resource.ResourceWithConfigValidators = (*instanceIPReverseDNSResource)(nil)
)

var reverseDNSTimeouts = common.Timeouts{Create: 10 * time.Minute, Update: 10 * time.Minute}

// The API lowercases a hostname and strips its trailing dot before checking it against this
// pattern; the validator requires the canonical spelling so the state holds what was configured.
var reverseDNSPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// NewInstanceIPReverseDNSResource returns the onetsolutions_instance_ip_reverse_dns resource.
func NewInstanceIPReverseDNSResource() resource.Resource {
	return &instanceIPReverseDNSResource{}
}

type instanceIPReverseDNSResource struct {
	common.ResourceBase
}

type instanceIPReverseDNSModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	InstanceID     types.String   `tfsdk:"instance_id"`
	IPAddressID    types.String   `tfsdk:"ip_address_id"`
	Address        types.String   `tfsdk:"address"`
	ReverseDNS     types.String   `tfsdk:"reverse_dns"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (m *instanceIPReverseDNSModel) fill(address *client.InstanceIPAddress) {
	m.ID = types.StringValue(address.ID)
	m.IPAddressID = types.StringValue(address.ID)
	m.Address = types.StringValue(address.Address)
	m.ReverseDNS = types.StringValue(address.ReverseDNS)
}

func (m *instanceIPReverseDNSModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (r *instanceIPReverseDNSResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_ip_reverse_dns"
}

func (r *instanceIPReverseDNSResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the reverse DNS (PTR) hostname of an IPv4 address of an instance.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("IP address"),
			"instance_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the instance the address is assigned to. Changing it targets another address (replacement).",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ip_address_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The UUID of the address. Exactly one of `ip_address_id` and `address` is set; the " +
					"other is computed. Setting a different one targets another address (replacement).",
				Validators: []validator.String{common.UUID()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"address": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The IPv4 address itself, such as `203.0.113.10`, resolved to its UUID among the " +
					"instance's addresses. Exactly one of `ip_address_id` and `address` is set; the other is computed. " +
					"Setting a different one targets another address (replacement).",
				Validators: []validator.String{ipv4Address{}},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"reverse_dns": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The fully qualified hostname, such as `mail.example.com`: lowercase, without a " +
					"trailing dot. It must resolve to the address. Can be changed in place.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(253),
					stringvalidator.RegexMatches(reverseDNSPattern,
						"must be a lowercase fully qualified hostname without a trailing dot, such as mail.example.com"),
				},
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, reverseDNSTimeouts),
		},
	}
}

func (r *instanceIPReverseDNSResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("ip_address_id"), path.MatchRoot("address")),
	}
}

func (r *instanceIPReverseDNSResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceIPReverseDNSModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, reverseDNSTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	address, diags := r.resolveAddress(ctx, c, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	updated, diags := r.setReverseDNS(ctx, c, &plan, address)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.fill(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// resolveAddress reads the address the configuration designates, by ID or by value.
func (r *instanceIPReverseDNSResource) resolveAddress(ctx context.Context, c *client.Client, plan *instanceIPReverseDNSModel) (*client.InstanceIPAddress, diag.Diagnostics) {
	var diags diag.Diagnostics
	scope, instanceID := plan.scope(), plan.InstanceID.ValueString()
	if !plan.IPAddressID.IsNull() && !plan.IPAddressID.IsUnknown() {
		address, err := c.GetInstanceIPAddress(ctx, scope, instanceID, plan.IPAddressID.ValueString())
		if err != nil {
			common.AddClientError(&diags, "Could not read the IP address", err)
			return nil, diags
		}
		return address, diags
	}
	addresses, err := c.ListInstanceIPAddresses(ctx, scope, instanceID)
	if err != nil {
		common.AddClientError(&diags, "Could not list the IP addresses of the instance", err)
		return nil, diags
	}
	wanted := plan.Address.ValueString()
	for i := range addresses {
		if addresses[i].Address == wanted {
			return &addresses[i], diags
		}
	}
	diags.AddAttributeError(path.Root("address"), "Unknown IP address",
		fmt.Sprintf("Instance %s has no address %s.", instanceID, wanted))
	return nil, diags
}

// setReverseDNS requests the hostname of the plan, retried while the instance is busy, waits for
// the update_reverse_dns task the API queues, and returns the address as it then is.
func (r *instanceIPReverseDNSResource) setReverseDNS(ctx context.Context, c *client.Client, plan *instanceIPReverseDNSModel, address *client.InstanceIPAddress) (*client.InstanceIPAddress, diag.Diagnostics) {
	var diags diag.Diagnostics
	scope, instanceID, hostname := plan.scope(), plan.InstanceID.ValueString(), plan.ReverseDNS.ValueString()
	if address.ReverseDNS == hostname {
		return address, diags
	}
	err := retryWhileBusy(ctx, r.Data().Waiter, "setting the reverse DNS of "+address.Address, func(ctx context.Context) error {
		_, err := c.UpdateReverseDNS(ctx, scope, instanceID, address.ID, hostname)
		return err
	})
	if err != nil {
		common.AddClientErrorWithFields(&diags, "Could not set the reverse DNS of "+address.Address, err, map[string]path.Path{
			"reverse_dns": path.Root("reverse_dns"),
		})
		return nil, diags
	}
	taskID, err := c.FindReverseDNSTask(ctx, scope, instanceID, address.ID, hostname)
	if err != nil {
		common.AddClientError(&diags, "Could not find the reverse DNS task", err)
		return nil, diags
	}
	if taskID != "" {
		if _, err = waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, taskID); err != nil {
			common.AddClientError(&diags, "The reverse DNS of "+address.Address+" was not set", err)
			return nil, diags
		}
	}
	updated, err := c.GetInstanceIPAddress(ctx, scope, instanceID, address.ID)
	if err != nil {
		common.AddClientError(&diags, "Could not read the IP address", err)
		return nil, diags
	}
	if updated.ReverseDNS != hostname {
		diags.AddAttributeError(path.Root("reverse_dns"), "Reverse DNS not set",
			fmt.Sprintf("The API reports %q as the reverse DNS of %s instead of %q.", updated.ReverseDNS, updated.Address, hostname))
		return nil, diags
	}
	return updated, diags
}

func (r *instanceIPReverseDNSResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceIPReverseDNSModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := instanceGone(ctx, c, state.scope(), state.InstanceID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the instance of the address", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	address, err := c.GetInstanceIPAddress(ctx, state.scope(), state.InstanceID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the IP address", err)
		return
	}
	state.fill(address)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *instanceIPReverseDNSResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state instanceIPReverseDNSModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpUpdate, reverseDNSTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.OrganizationID, plan.ProjectID = state.OrganizationID, state.ProjectID
	address, err := c.GetInstanceIPAddress(ctx, state.scope(), state.InstanceID.ValueString(), state.ID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the IP address", err)
		return
	}
	updated, diags := r.setReverseDNS(ctx, c, &plan, address)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.fill(updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete only forgets the resource: the API has no way to reset a reverse DNS.
func (r *instanceIPReverseDNSResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *instanceIPReverseDNSResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScopedChild(ctx, req, resp, "instance_id")
}

// ipv4Address validates an IPv4 address in its canonical dotted form.
type ipv4Address struct{}

func (ipv4Address) Description(context.Context) string {
	return "value must be an IPv4 address such as 203.0.113.10"
}

func (v ipv4Address) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (ipv4Address) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	parsed, err := netip.ParseAddr(value)
	if err != nil || !parsed.Is4() || parsed.String() != value {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid IPv4 address",
			fmt.Sprintf("%q is not an IPv4 address such as 203.0.113.10. Reverse DNS can only be set on IPv4 addresses.", value))
	}
}
