package dns

import (
	"context"

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
)

var (
	_ resource.ResourceWithConfigure   = (*dnssecResource)(nil)
	_ resource.ResourceWithImportState = (*dnssecResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*dnssecResource)(nil)
)

// DNSSECWarning is the API's warning, repeated on the resource page.
const DNSSECWarning = "Before disabling DNSSEC (setting `enabled = false` or destroying this resource), remove the " +
	"DS record from the domain's registrar and wait for its TTL to expire: a domain whose registrar still publishes a " +
	"DS record for a zone that is no longer signed stops resolving."

// NewDNSSECResource returns the onetsolutions_dns_zone_dnssec resource.
func NewDNSSECResource() resource.Resource {
	return &dnssecResource{}
}

type dnssecResource struct {
	common.ResourceBase
}

type dnssecModel struct {
	ID                     types.String `tfsdk:"id"`
	OrganizationID         types.String `tfsdk:"organization_id"`
	ProjectID              types.String `tfsdk:"project_id"`
	ZoneID                 types.String `tfsdk:"zone_id"`
	Enabled                types.Bool   `tfsdk:"enabled"`
	Status                 types.String `tfsdk:"status"`
	DSRecord               types.String `tfsdk:"ds_record"`
	KeyTag                 types.Int64  `tfsdk:"key_tag"`
	Algorithm              types.String `tfsdk:"algorithm"`
	DigestType             types.String `tfsdk:"digest_type"`
	Digest                 types.String `tfsdk:"digest"`
	PublicKey              types.String `tfsdk:"public_key"`
	Flags                  types.Int64  `tfsdk:"flags"`
	AutomaticDSPublication types.Bool   `tfsdk:"automatic_ds_publication"`
}

func (m *dnssecModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

// fill writes the API's DNSSEC state. enabled follows the status, except while it is "error",
// when it keeps the value the model holds.
func (m *dnssecModel) fill(state *client.DNSSEC, zoneID string, scope client.Scope) {
	m.ID = types.StringValue(zoneID)
	m.ZoneID = types.StringValue(zoneID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	switch state.Status {
	case client.DNSSECActive, client.DNSSECPending:
		m.Enabled = types.BoolValue(true)
	case client.DNSSECDisabled, client.DNSSECPendingDisabled:
		m.Enabled = types.BoolValue(false)
	default:
		if m.Enabled.IsNull() || m.Enabled.IsUnknown() {
			m.Enabled = types.BoolValue(false)
		}
	}
	m.Status = types.StringValue(state.Status)
	m.DSRecord = types.StringPointerValue(state.DSRecord)
	m.KeyTag = types.Int64PointerValue(state.KeyTag)
	m.Algorithm = types.StringPointerValue(state.Algorithm)
	m.DigestType = types.StringPointerValue(state.DigestType)
	m.Digest = types.StringPointerValue(state.Digest)
	m.PublicKey = types.StringPointerValue(state.PublicKey)
	m.Flags = types.Int64PointerValue(state.Flags)
	m.AutomaticDSPublication = types.BoolValue(state.AutomaticDSPublication)
}

func (r *dnssecResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone_dnssec"
}

func (r *dnssecResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the DNSSEC signing of a DNS zone.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The UUID of the zone, the identifier of its DNSSEC state.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the zone (see `onetsolutions_dns_zone`). Changing it recreates the resource.",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Required:            true,
				MarkdownDescription: "Whether the zone is signed. Read the warning above before setting it to `false`.",
			},
			"status": computed("`active`, `pending` (signed, waiting for the DS record at the registrar), `disabled`, " +
				"`pending_disabled` or `error`."),
			"ds_record": computed("The DS record to publish at the registrar, in zone-file form."),
			"key_tag": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The key tag of the DS record.",
			},
			"algorithm":   computed("The DNSSEC algorithm number of the key."),
			"digest_type": computed("The digest type of the DS record."),
			"digest":      computed("The digest of the DS record."),
			"public_key":  computed("The DNSKEY public key, for registrars that ask for it instead of a DS record."),
			"flags": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The flags of the DNSKEY record.",
			},
			"automatic_ds_publication": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the DS record is published at the registrar automatically.",
			},
		}),
	}
}

func (r *dnssecResource) apply(ctx context.Context, plan *dnssecModel, scope client.Scope, diags *diag.Diagnostics) {
	c := r.Client(diags)
	if c == nil {
		return
	}
	state, err := c.UpdateDNSSEC(ctx, scope, plan.ZoneID.ValueString(), plan.Enabled.ValueBool())
	if err != nil {
		common.AddClientError(diags, "Could not change the DNSSEC state of the zone", err)
		return
	}
	plan.fill(state, plan.ZoneID.ValueString(), scope)
}

func (r *dnssecResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnssecModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := r.ResolveScope(ctx, plan.OrganizationID, plan.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, scope, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnssecResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnssecModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	zoneID := state.ZoneID.ValueString()
	if zoneID == "" {
		zoneID = state.ID.ValueString()
	}
	current, err := c.GetDNSSEC(ctx, state.scope(), zoneID)
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the DNSSEC state of the zone", err)
		return
	}
	state.fill(current, zoneID, state.scope())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *dnssecResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dnssecModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, state.scope(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete disables DNSSEC; a zone already deleted has nothing left to disable.
func (r *dnssecResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnssecModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := c.UpdateDNSSEC(ctx, state.scope(), state.ZoneID.ValueString(), false)
	if err != nil && !client.IsNotFound(err) {
		common.AddClientError(&resp.Diagnostics, "Could not disable DNSSEC on the zone", err)
	}
}

// ModifyPlan warns whenever the plan turns DNSSEC off.
func (r *dnssecResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if req.State.Raw.IsNull() {
		return
	}
	var stateEnabled types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("enabled"), &stateEnabled)...)
	if !stateEnabled.ValueBool() {
		return
	}
	disabling := req.Plan.Raw.IsNull()
	if !disabling {
		var planEnabled types.Bool
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("enabled"), &planEnabled)...)
		disabling = !planEnabled.IsUnknown() && !planEnabled.ValueBool()
	}
	if disabling {
		resp.Diagnostics.AddWarning("This plan disables DNSSEC", DNSSECWarning)
	}
}

// ImportState takes "<organization_id>/<project_id>/<zone_id>" or "<zone_id>".
func (r *dnssecResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var id types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("id"), &id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), id)...)
}
