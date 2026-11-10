package catalog

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var _ datasource.DataSourceWithConfigure = (*controlPanelsDataSource)(nil)

// NewControlPanelsDataSource returns the onetsolutions_control_panels data source.
func NewControlPanelsDataSource() datasource.DataSource {
	return &controlPanelsDataSource{}
}

type controlPanelsDataSource struct {
	common.DataSourceBase
}

type controlPanelModel struct {
	ID                 types.String `tfsdk:"id"`
	Slug               types.String `tfsdk:"slug"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	AccessPort         types.Int64  `tfsdk:"access_port"`
	CompatibleImageIDs types.List   `tfsdk:"compatible_image_ids"`
	MinVCPU            types.Int64  `tfsdk:"min_vcpu"`
	MinRAMMB           types.Int64  `tfsdk:"min_ram_mb"`
	MinDiskGB          types.Int64  `tfsdk:"min_disk_gb"`
	InstallMinutesMin  types.Int64  `tfsdk:"install_minutes_min"`
	InstallMinutesMax  types.Int64  `tfsdk:"install_minutes_max"`
	DocsURL            types.String `tfsdk:"docs_url"`
}

var controlPanelAttrTypes = map[string]attr.Type{
	"id": types.StringType, "slug": types.StringType, "name": types.StringType, "description": types.StringType,
	"access_port": types.Int64Type, "compatible_image_ids": types.ListType{ElemType: types.StringType},
	"min_vcpu": types.Int64Type, "min_ram_mb": types.Int64Type, "min_disk_gb": types.Int64Type,
	"install_minutes_min": types.Int64Type, "install_minutes_max": types.Int64Type, "docs_url": types.StringType,
}

type controlPanelsModel struct {
	ControlPanels types.List `tfsdk:"control_panels"`
}

func (d *controlPanelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_control_panels"
}

func (d *controlPanelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the control panels an instance can be installed with (its `control_panel` input).",
		Attributes: map[string]schema.Attribute{
			"control_panels": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The control panels.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the control panel."},
					"slug":        schema.StringAttribute{Computed: true, MarkdownDescription: "The slug, for the instance's `control_panel`."},
					"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "The name of the control panel."},
					"description": schema.StringAttribute{Computed: true, MarkdownDescription: "What the control panel does."},
					"access_port": schema.Int64Attribute{Computed: true, MarkdownDescription: "The TCP port its web interface listens on."},
					"compatible_image_ids": schema.ListAttribute{
						Computed:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "The UUIDs of the images it installs on.",
					},
					"min_vcpu":            schema.Int64Attribute{Computed: true, MarkdownDescription: "The fewest virtual CPUs it needs."},
					"min_ram_mb":          schema.Int64Attribute{Computed: true, MarkdownDescription: "The least memory it needs, in MB."},
					"min_disk_gb":         schema.Int64Attribute{Computed: true, MarkdownDescription: "The smallest disk it needs, in GB."},
					"install_minutes_min": schema.Int64Attribute{Computed: true, MarkdownDescription: "The shortest installation time, in minutes."},
					"install_minutes_max": schema.Int64Attribute{Computed: true, MarkdownDescription: "The longest installation time, in minutes."},
					"docs_url":            schema.StringAttribute{Computed: true, MarkdownDescription: "The documentation of the control panel."},
				}},
			},
		},
	}
}

func (d *controlPanelsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	panels, err := c.ListControlPanels(ctx)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the control panels", err)
		return
	}
	models := make([]controlPanelModel, 0, len(panels))
	for i := range panels {
		p := &panels[i]
		ids := make([]string, 0, len(p.CompatibleImages))
		for _, image := range p.CompatibleImages {
			ids = append(ids, image.ID)
		}
		imageIDs, diags := types.ListValueFrom(ctx, types.StringType, ids)
		resp.Diagnostics.Append(diags...)
		models = append(models, controlPanelModel{
			ID: types.StringValue(p.ID), Slug: types.StringValue(p.Slug), Name: types.StringValue(p.Name),
			Description: types.StringValue(p.Description), AccessPort: types.Int64Value(p.AccessPort),
			CompatibleImageIDs: imageIDs, MinVCPU: types.Int64Value(p.MinVCPU), MinRAMMB: types.Int64Value(p.MinRAMMB),
			MinDiskGB: types.Int64Value(p.MinDiskGB), InstallMinutesMin: types.Int64Value(p.InstallMinutesMin),
			InstallMinutesMax: types.Int64Value(p.InstallMinutesMax), DocsURL: types.StringValue(p.DocsURL),
		})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: controlPanelAttrTypes}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, controlPanelsModel{ControlPanels: list})...)
}
