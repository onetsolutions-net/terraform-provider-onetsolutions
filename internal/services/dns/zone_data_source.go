package dns

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ datasource.DataSourceWithConfigure        = (*zoneDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*zoneDataSource)(nil)
)

// NewZoneDataSource returns the onetsolutions_dns_zone data source.
func NewZoneDataSource() datasource.DataSource {
	return &zoneDataSource{}
}

type zoneDataSource struct {
	common.DataSourceBase
}

type zoneDataModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	ZoneName       types.String `tfsdk:"zone_name"`
	Mode           types.String `tfsdk:"mode"`
	Nameservers    types.List   `tfsdk:"nameservers"`
	IsVerified     types.Bool   `tfsdk:"is_verified"`
	DomainID       types.String `tfsdk:"domain_id"`
	RecordCount    types.Int64  `tfsdk:"record_count"`
	Created        types.String `tfsdk:"created"`
}

func (d *zoneDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (d *zoneDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a DNS zone of a project by `id` or by `zone_name`.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the zone. Exactly one of `id` and `zone_name` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"zone_name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The domain the zone serves, lowercase without a trailing dot. Exactly one of `id` and `zone_name` is required.",
				Validators:          []validator.String{zoneNameValidator{}},
			},
			"mode": schema.StringAttribute{Computed: true, MarkdownDescription: "`onetsolutions` (hosted) or `client` (hosted elsewhere)."},
			"nameservers": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The name servers assigned to the zone.",
			},
			"is_verified":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the domain is delegated to the zone's name servers."},
			"domain_id":    schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the domain the zone serves, when there is one."},
			"record_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "The number of records in the zone."},
			"created":      schema.StringAttribute{Computed: true, MarkdownDescription: "When the zone was created (RFC 3339)."},
		}),
	}
}

func (d *zoneDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("zone_name")),
	}
}

func (d *zoneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config zoneDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := d.ResolveScope(ctx, config.OrganizationID, config.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := config.ID.ValueString()
	if config.ID.IsNull() {
		zones, err := c.ListDNSZones(ctx, scope)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the DNS zones", err)
			return
		}
		name := config.ZoneName.ValueString()
		found, findDiags := common.FindOne(zones,
			func(z client.DNSZone) bool { return z.ZoneName == name },
			func(z client.DNSZone) string { return z.ID },
			"DNS zone", fmt.Sprintf("zone_name %q", name))
		resp.Diagnostics.Append(findDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		id = found.ID
	}
	zone, err := c.GetDNSZone(ctx, scope, id)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the DNS zone", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, zoneDataModel{
		ID:             types.StringValue(zone.ID),
		OrganizationID: types.StringValue(scope.OrganizationID),
		ProjectID:      types.StringValue(scope.ProjectID),
		ZoneName:       types.StringValue(zone.ZoneName),
		Mode:           types.StringValue(zone.Mode),
		Nameservers:    stringListValue(ctx, zone.NameserverList()),
		IsVerified:     types.BoolValue(zone.IsVerified),
		DomainID:       types.StringPointerValue(zone.DomainID),
		RecordCount:    types.Int64Value(zone.RecordCount),
		Created:        types.StringValue(zone.Created),
	})...)
}
