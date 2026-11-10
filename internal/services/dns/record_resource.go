package dns

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure      = (*recordResource)(nil)
	_ resource.ResourceWithImportState    = (*recordResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*recordResource)(nil)
	_ resource.ResourceWithValidateConfig = (*recordResource)(nil)
)

var recordTimeouts = common.Timeouts{Update: 5 * time.Minute, Delete: 5 * time.Minute}

// The record types a customer can manage; SOA belongs to the DNS service.
var recordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA", "PTR"}

var (
	proxiableTypes = []string{"A", "AAAA", "CNAME"}
	priorityTypes  = []string{"MX", "SRV"}
	automaticTTL   = int64(1)
	minExplicitTTL = int64(60)
	maxTTL         = int64(86400)
)

// NewRecordResource returns the onetsolutions_dns_record resource.
func NewRecordResource() resource.Resource {
	return &recordResource{}
}

type recordResource struct {
	common.ResourceBase
}

type recordModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	ZoneID         types.String   `tfsdk:"zone_id"`
	Type           types.String   `tfsdk:"type"`
	Name           recordString   `tfsdk:"name"`
	Content        recordString   `tfsdk:"content"`
	TTL            types.Int64    `tfsdk:"ttl"`
	Priority       types.Int64    `tfsdk:"priority"`
	Proxied        types.Bool     `tfsdk:"proxied"`
	Created        types.String   `tfsdk:"created"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (m *recordModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

// fill writes the API's record into the model. The name and the content keep the spelling the
// model already holds (from the configuration or the state) when the API's value is the same
// record in its canonical form; otherwise they take the API's value, in its import spelling
// when the model holds none.
func (m *recordModel) fill(record *client.DNSRecord, zoneName string, scope client.Scope) {
	m.ID = types.StringValue(record.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.Type = types.StringValue(record.RecordType)
	if m.Name.IsNull() || m.Name.IsUnknown() || !sameName(m.Name.ValueString(), record.Name, zoneName) {
		m.Name = newName(importedName(record.Name, zoneName))
	}
	if m.Content.IsNull() || m.Content.IsUnknown() || !sameContent(record.RecordType, m.Content.ValueString(), record.Content) {
		m.Content = newContent(importedContent(record.RecordType, record.Content))
	}
	m.TTL = types.Int64Value(record.TTL)
	switch {
	case record.Priority == nil:
		m.Priority = types.Int64Null()
	case m.Priority.IsNull() && !slices.Contains(priorityTypes, record.RecordType):
		// The DNS service may return a priority for types that have none; it is not drift.
	default:
		m.Priority = types.Int64PointerValue(record.Priority)
	}
	m.Proxied = types.BoolValue(record.Proxied)
	m.Created = types.StringValue(record.Created)
}

func (r *recordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (r *recordResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a record in a DNS zone.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("record"),
			"zone_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the zone (see `onetsolutions_dns_zone`). Changing it recreates the record.",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The record type, one of `" + strings.Join(recordTypes, "`, `") + "`. Changing it " +
					"recreates the record.",
				Validators:    []validator.String{stringvalidator.OneOf(recordTypes...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:   true,
				CustomType: nameType,
				MarkdownDescription: "The name of the record: `@` (or the zone name) for the apex, a name relative to the " +
					"zone such as `www`, or a fully qualified name inside the zone.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 253)},
			},
			"content": schema.StringAttribute{
				Required:   true,
				CustomType: contentType,
				MarkdownDescription: "The value of the record: an IPv4 address for `A`, an IPv6 address for `AAAA`, a host " +
					"name for `CNAME`, `MX`, `NS` and `PTR`, the text for `TXT` (quoted or not), `weight port target` for " +
					"`SRV`, `flags tag \"value\"` for `CAA`.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"ttl": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The time to live, in seconds: `1` (automatic) or between 60 and 86400. When unset, " +
					"defaults to 3600, or to automatic on a proxied record, which only accepts `1`.",
				Validators: []validator.Int64{ttlValidator{}},
			},
			"priority": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "The priority, between 0 and 65535; required for `MX` and `SRV` records.",
				Validators:          []validator.Int64{int64validator.Between(0, 65535)},
			},
			"proxied": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Route the traffic through the edge network (`A`, `AAAA` and `CNAME` only). Defaults " +
					"to `false`.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the record was created (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, recordTimeouts),
		},
	}
}

func (r *recordResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config recordModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Type.IsUnknown() || config.Type.IsNull() {
		return
	}
	recordType := config.Type.ValueString()
	if slices.Contains(priorityTypes, recordType) && config.Priority.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("priority"), "Missing priority",
			fmt.Sprintf("A %s record needs a priority.", recordType))
	}
	if config.Proxied.ValueBool() && !slices.Contains(proxiableTypes, recordType) {
		resp.Diagnostics.AddAttributeError(path.Root("proxied"), "Record cannot be proxied",
			"Only A, AAAA and CNAME records can be proxied.")
	}
	if config.Proxied.ValueBool() && !config.TTL.IsNull() && !config.TTL.IsUnknown() && config.TTL.ValueInt64() != automaticTTL {
		resp.Diagnostics.AddAttributeError(path.Root("ttl"), "Invalid TTL for a proxied record",
			"A proxied record has an automatic TTL: remove ttl or set it to 1.")
	}
	if config.Content.IsUnknown() || config.Content.IsNull() {
		return
	}
	content := strings.TrimSpace(config.Content.ValueString())
	switch recordType {
	case "A":
		if addr, err := netip.ParseAddr(content); err != nil || !addr.Is4() {
			resp.Diagnostics.AddAttributeError(path.Root("content"), "Invalid content", fmt.Sprintf("%q is not an IPv4 address.", content))
		}
	case "AAAA":
		if addr, err := netip.ParseAddr(content); err != nil || !addr.Is6() || addr.Is4In6() {
			resp.Diagnostics.AddAttributeError(path.Root("content"), "Invalid content", fmt.Sprintf("%q is not an IPv6 address.", content))
		}
	}
}

// ModifyPlan plans ttl when the configuration leaves it unset: automatic for a proxied record,
// the state's value while proxied does not change, otherwise whatever the API chooses.
func (r *recordResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if req.Plan.Raw.IsNull() {
		return
	}
	var configTTL types.Int64
	var planProxied types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("ttl"), &configTTL)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("proxied"), &planProxied)...)
	if resp.Diagnostics.HasError() || !configTTL.IsNull() {
		return
	}
	switch {
	case planProxied.IsUnknown():
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ttl"), types.Int64Unknown())...)
	case planProxied.ValueBool():
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ttl"), types.Int64Value(automaticTTL))...)
	case !req.State.Raw.IsNull():
		var stateTTL types.Int64
		var stateProxied types.Bool
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("ttl"), &stateTTL)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("proxied"), &stateProxied)...)
		if !stateProxied.ValueBool() {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ttl"), stateTTL)...)
		}
	}
}

func (m *recordModel) write() client.DNSRecordWrite {
	in := client.DNSRecordWrite{
		RecordType: m.Type.ValueString(),
		Name:       m.Name.ValueString(),
		Content:    m.Content.ValueString(),
		Priority:   m.Priority.ValueInt64Pointer(),
		Proxied:    m.Proxied.ValueBool(),
	}
	if !m.TTL.IsNull() && !m.TTL.IsUnknown() {
		in.TTL = m.TTL.ValueInt64Pointer()
	}
	return in
}

var recordFieldPaths = map[string]path.Path{
	"record_type": path.Root("type"), "name": path.Root("name"), "content": path.Root("content"),
	"ttl": path.Root("ttl"), "priority": path.Root("priority"), "proxied": path.Root("proxied"),
}

func (r *recordResource) zoneName(ctx context.Context, c *client.Client, scope client.Scope, zoneID string) (string, error) {
	zone, err := c.GetDNSZone(ctx, scope, zoneID)
	if err != nil {
		return "", err
	}
	return zone.ZoneName, nil
}

func (r *recordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan recordModel
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
	zoneName, err := r.zoneName(ctx, c, scope, plan.ZoneID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the DNS zone of the record", err)
		return
	}
	record, err := c.CreateDNSRecord(ctx, scope, plan.ZoneID.ValueString(), plan.write())
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the DNS record", err, recordFieldPaths)
		return
	}
	plan.fill(record, zoneName, scope)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *recordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state recordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	zoneName, err := r.zoneName(ctx, c, state.scope(), state.ZoneID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the DNS zone of the record", err)
		return
	}
	record, err := c.GetDNSRecord(ctx, state.scope(), state.ZoneID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the DNS record", err)
		return
	}
	state.fill(record, zoneName, state.scope())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *recordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state recordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpUpdate, recordTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := state.scope()
	zoneName, err := r.zoneName(ctx, c, scope, state.ZoneID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the DNS zone of the record", err)
		return
	}
	var record *client.DNSRecord
	err = waiter.Retry(ctx, r.Data().Waiter, "updating DNS record "+state.ID.ValueString(), func(ctx context.Context) error {
		updated, updateErr := c.UpdateDNSRecord(ctx, scope, state.ZoneID.ValueString(), state.ID.ValueString(), plan.write())
		record = updated
		return updateErr
	}, dnsServiceBusy)
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not update the DNS record", err, recordFieldPaths)
		return
	}
	plan.fill(record, zoneName, scope)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *recordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state recordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, recordTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := waiter.Retry(ctx, r.Data().Waiter, "deleting DNS record "+state.ID.ValueString(), func(ctx context.Context) error {
		err := c.DeleteDNSRecord(ctx, state.scope(), state.ZoneID.ValueString(), state.ID.ValueString())
		if client.IsNotFound(err) {
			return nil
		}
		return err
	}, dnsServiceBusy)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not delete the DNS record", err)
	}
}

func (r *recordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScopedChild(ctx, req, resp, "zone_id")
}

type ttlValidator struct{}

func (ttlValidator) Description(context.Context) string {
	return fmt.Sprintf("value must be %d (automatic) or between %d and %d", automaticTTL, minExplicitTTL, maxTTL)
}

func (v ttlValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v ttlValidator) ValidateInt64(ctx context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	ttl := req.ConfigValue.ValueInt64()
	if ttl != automaticTTL && (ttl < minExplicitTTL || ttl > maxTTL) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid TTL", fmt.Sprintf("%d: the %s.", ttl, v.Description(ctx)))
	}
}
