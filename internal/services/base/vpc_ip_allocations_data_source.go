package base

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var _ datasource.DataSourceWithConfigure = (*vpcIPAllocationsDataSource)(nil)

// NewVPCIPAllocationsDataSource returns the onetsolutions_vpc_ip_allocations data source.
func NewVPCIPAllocationsDataSource() datasource.DataSource {
	return &vpcIPAllocationsDataSource{}
}

type vpcIPAllocationsDataSource struct {
	common.DataSourceBase
}

type vpcIPAllocationsModel struct {
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	VPCID          types.String `tfsdk:"vpc_id"`
	Allocations    types.List   `tfsdk:"allocations"`
}

var vpcIPAllocationAttrTypes = map[string]attr.Type{
	"id":              types.StringType,
	"ip_address":      types.StringType,
	"instance_id":     types.StringType,
	"instance_name":   types.StringType,
	"status":          types.StringType,
	"allocation_type": types.StringType,
	"interface_index": types.Int64Type,
	"allocated_at":    types.StringType,
}

func (d *vpcIPAllocationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_ip_allocations"
}

func (d *vpcIPAllocationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the private IP addresses allocated in a VPC, ordered by address.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"vpc_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the VPC.",
				Validators:          []validator.String{common.UUID()},
			},
			"allocations": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The addresses reserved or allocated in the VPC; released ones are not listed.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":         schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the allocation."},
					"ip_address": schema.StringAttribute{Computed: true, MarkdownDescription: "The private IPv4 address."},
					"instance_id": schema.StringAttribute{Computed: true,
						MarkdownDescription: "The UUID of the instance using the address, null for the gateway's."},
					"instance_name": schema.StringAttribute{Computed: true,
						MarkdownDescription: "The name of the instance using the address, null for the gateway's."},
					"status": schema.StringAttribute{Computed: true, MarkdownDescription: "`reserved` or `allocated`."},
					"allocation_type": schema.StringAttribute{Computed: true,
						MarkdownDescription: "`instance` for an instance's address, `gateway` for the VPC's gateway."},
					"interface_index": schema.Int64Attribute{Computed: true,
						MarkdownDescription: "The private network interface the address is on: `1` for the first."},
					"allocated_at": schema.StringAttribute{Computed: true, MarkdownDescription: "When the address was allocated (RFC 3339)."},
				}},
			},
		}),
	}
}

func (d *vpcIPAllocationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config vpcIPAllocationsModel
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
	allocations, err := c.ListVPCIPAllocations(ctx, scope, config.VPCID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the IP addresses of the VPC", err)
		return
	}
	items := make([]attr.Value, 0, len(allocations))
	for _, a := range allocations {
		item, itemDiags := types.ObjectValue(vpcIPAllocationAttrTypes, map[string]attr.Value{
			"id":              types.StringValue(a.ID),
			"ip_address":      types.StringValue(a.IPAddress),
			"instance_id":     types.StringPointerValue(a.InstanceID),
			"instance_name":   types.StringPointerValue(a.InstanceName),
			"status":          types.StringValue(a.Status),
			"allocation_type": types.StringValue(a.AllocationType),
			"interface_index": types.Int64Value(a.InterfaceIndex),
			"allocated_at":    types.StringValue(a.AllocatedAt),
		})
		resp.Diagnostics.Append(itemDiags...)
		items = append(items, item)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: vpcIPAllocationAttrTypes}, items)
	resp.Diagnostics.Append(diags...)
	config.OrganizationID = types.StringValue(scope.OrganizationID)
	config.ProjectID = types.StringValue(scope.ProjectID)
	config.Allocations = list
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
