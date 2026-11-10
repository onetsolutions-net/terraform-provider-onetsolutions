package base

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
	_ datasource.DataSourceWithConfigure        = (*vpcDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*vpcDataSource)(nil)
)

// NewVPCDataSource returns the onetsolutions_vpc data source.
func NewVPCDataSource() datasource.DataSource {
	return &vpcDataSource{}
}

type vpcDataSource struct {
	common.DataSourceBase
}

type vpcDataModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	Name           types.String `tfsdk:"name"`
	CIDRBlock      types.String `tfsdk:"cidr_block"`
	Created        types.String `tfsdk:"created"`
}

func (d *vpcDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

func (d *vpcDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a VPC of a project by `id` or by `name`.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the VPC. Exactly one of `id` and `name` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The name of the VPC. Exactly one of `id` and `name` is required.",
			},
			"cidr_block": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The private IPv4 range of the VPC in CIDR notation.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the VPC was created (RFC 3339).",
			},
		}),
	}
}

func (d *vpcDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *vpcDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config vpcDataModel
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

	var vpc *client.VPC
	if !config.ID.IsNull() {
		found, err := c.GetVPC(ctx, scope, config.ID.ValueString())
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the VPC", err)
			return
		}
		vpc = found
	} else {
		vpcs, err := c.ListVPCs(ctx, scope)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the VPCs", err)
			return
		}
		name := config.Name.ValueString()
		found, diags := common.FindOne(vpcs,
			func(v client.VPC) bool { return v.Name == name },
			func(v client.VPC) string { return v.ID },
			"VPC", fmt.Sprintf("name %q", name))
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		vpc = &found
	}

	model := vpcDataModel{
		ID:             types.StringValue(vpc.ID),
		OrganizationID: types.StringValue(scope.OrganizationID),
		ProjectID:      types.StringValue(scope.ProjectID),
		Name:           types.StringValue(vpc.Name),
		CIDRBlock:      types.StringValue(vpc.CIDRBlock),
		Created:        types.StringValue(vpc.Created),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
