package dns

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*zoneResource)(nil)
	_ resource.ResourceWithImportState = (*zoneResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*zoneResource)(nil)
)

var zoneTimeouts = common.Timeouts{Create: 10 * time.Minute, Delete: 5 * time.Minute}

const (
	zoneModeHosted = "onetsolutions"
	zoneModeClient = "client"
)

// NewZoneResource returns the onetsolutions_dns_zone resource.
func NewZoneResource() resource.Resource {
	return &zoneResource{}
}

type zoneResource struct {
	common.ResourceBase
}

type zoneModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	ZoneName       types.String   `tfsdk:"zone_name"`
	Mode           types.String   `tfsdk:"mode"`
	Nameservers    types.List     `tfsdk:"nameservers"`
	IsVerified     types.Bool     `tfsdk:"is_verified"`
	DomainID       types.String   `tfsdk:"domain_id"`
	RecordCount    types.Int64    `tfsdk:"record_count"`
	Created        types.String   `tfsdk:"created"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (m *zoneModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (m *zoneModel) fill(ctx context.Context, zone *client.DNSZone, scope client.Scope) {
	m.ID = types.StringValue(zone.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.ZoneName = types.StringValue(zone.ZoneName)
	m.Mode = types.StringValue(zone.Mode)
	m.Nameservers = stringListValue(ctx, zone.NameserverList())
	m.IsVerified = types.BoolValue(zone.IsVerified)
	m.DomainID = types.StringPointerValue(zone.DomainID)
	m.RecordCount = types.Int64Value(zone.RecordCount)
	m.Created = types.StringValue(zone.Created)
}

func stringListValue(_ context.Context, values []string) types.List {
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elements)
}

func (r *zoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (r *zoneResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a DNS zone hosted by OnetSolutions.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("DNS zone"),
			"zone_name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The registered domain the zone serves, such as `example.com`: lowercase, without a " +
					"trailing dot, so the state always equals the configuration. Changing it " +
					"recreates the zone.",
				Validators:    []validator.String{zoneNameValidator{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"mode": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(zoneModeHosted),
				MarkdownDescription: "`onetsolutions` (default) hosts the zone on the OnetSolutions name servers. `client` " +
					"marks a zone hosted elsewhere: such a zone is added by importing it in the console and can only be " +
					"imported here, not created. Changing it recreates the zone.",
				Validators:    []validator.String{stringvalidator.OneOf(zoneModeHosted, zoneModeClient)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"nameservers": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The name servers assigned to the zone, to delegate the domain to.",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"is_verified": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the domain is delegated to the zone's name servers.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"domain_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The UUID of the domain of the project the zone serves, when there is one.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"record_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The number of records in the zone, including the SOA and NS records managed by OnetSolutions.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the zone was created (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, zoneTimeouts),
		},
	}
}

func (r *zoneResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if req.Plan.Raw.IsNull() || !req.State.Raw.IsNull() {
		return
	}
	var mode types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("mode"), &mode)...)
	if mode.ValueString() == zoneModeClient {
		resp.Diagnostics.AddAttributeError(path.Root("mode"), "A client zone cannot be created",
			"A zone hosted elsewhere (mode = \"client\") is added by importing it in the console; import that zone with "+
				"terraform import instead of creating it.")
	}
}

func (r *zoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan zoneModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, zoneTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	zone, err := c.CreateDNSZone(ctx, scope, client.DNSZoneCreate{ZoneName: plan.ZoneName.ValueString(), Mode: plan.Mode.ValueString()})
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the DNS zone", err, map[string]path.Path{
			"zone_name": path.Root("zone_name"), "mode": path.Root("mode"),
		})
		return
	}
	resp.Diagnostics.Append(common.SavePartialState(ctx, &resp.State, zone.ID, scope, map[string]attr.Value{
		"zone_name": plan.ZoneName, "mode": plan.Mode, "timeouts": plan.Timeouts,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	provisioned, err := waiter.Wait[*client.DNSZone]{
		Description: fmt.Sprintf("DNS zone %s to get its name servers", plan.ZoneName.ValueString()),
		Target:      []string{"provisioned"},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.DNSZone, string, error) {
			current, getErr := c.GetDNSZone(ctx, scope, zone.ID)
			if getErr != nil {
				return nil, "", getErr
			}
			if len(current.NameserverList()) == 0 {
				return current, "provisioning", nil
			}
			return current, "provisioned", nil
		},
	}.Run(ctx)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "The DNS zone was created but is not provisioned", err)
		return
	}
	plan.fill(ctx, provisioned, scope)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *zoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state zoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	zone, err := c.GetDNSZone(ctx, state.scope(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the DNS zone", err)
		return
	}
	state.fill(ctx, zone, state.scope())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only changes the timeouts: every other input forces a replacement.
func (r *zoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan zoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *zoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state zoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, zoneTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := waiter.Retry(ctx, r.Data().Waiter, "deleting DNS zone "+state.ZoneName.ValueString(),
		func(ctx context.Context) error {
			err := c.DeleteDNSZone(ctx, state.scope(), state.ID.ValueString())
			if client.IsNotFound(err) {
				return nil
			}
			return err
		},
		dnsServiceBusy)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not delete the DNS zone", err)
	}
}

func (r *zoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
}

// dnsServiceBusy matches the answers of a DNS operation worth retrying: 409 (an operation in
// progress) and 503 (the DNS service did not answer in time).
func dnsServiceBusy(err error) bool {
	apiErr, ok := client.AsAPIError(err)
	return ok && (apiErr.StatusCode == http.StatusConflict || apiErr.StatusCode == http.StatusServiceUnavailable)
}

type zoneNameValidator struct{}

func (zoneNameValidator) Description(context.Context) string {
	return "value must be a lowercase domain name without a trailing dot"
}

func (v zoneNameValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (zoneNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := checkDomainName(req.ConfigValue.ValueString(), 2); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid zone name", err.Error())
	}
}

// checkDomainName checks a domain name is in the API's canonical form: lowercase labels of
// letters, digits and hyphens, at least minLabels of them, no trailing dot.
func checkDomainName(value string, minLabels int) error {
	if value != strings.ToLower(value) {
		return fmt.Errorf("%q must be lowercase: write %q", value, strings.ToLower(value))
	}
	if strings.HasSuffix(value, ".") {
		return fmt.Errorf("%q must not end with a dot: write %q", value, strings.TrimSuffix(value, "."))
	}
	if len(value) > 253 {
		return fmt.Errorf("%q is longer than 253 characters", value)
	}
	labels := strings.Split(value, ".")
	if len(labels) < minLabels {
		return fmt.Errorf("%q is not a domain name such as example.com", value)
	}
	for _, label := range labels {
		if !isLabel(label) {
			return fmt.Errorf("%q is not a domain name such as example.com: label %q is invalid", value, label)
		}
	}
	return nil
}

func isLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
