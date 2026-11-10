package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ datasource.DataSourceWithConfigure        = (*instanceSizeDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*instanceSizeDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*instanceSizesDataSource)(nil)
)

type instanceSizeModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Slug          types.String `tfsdk:"slug"`
	CPUModel      types.String `tfsdk:"cpu_model"`
	VCPU          types.Int64  `tfsdk:"vcpu"`
	RAMMB         types.Int64  `tfsdk:"ram_mb"`
	BandwidthMbps types.Int64  `tfsdk:"bandwidth_mbps"`
	Description   types.String `tfsdk:"description"`
	Volumes       types.List   `tfsdk:"volumes"`
	Pricing       types.List   `tfsdk:"pricing"`
	Addons        types.List   `tfsdk:"addons"`
}

type sizeVolumeModel struct {
	ID         types.String `tfsdk:"id"`
	VolumeType types.String `tfsdk:"volume_type"`
	DiskGB     types.Int64  `tfsdk:"disk_gb"`
	IsDefault  types.Bool   `tfsdk:"is_default"`
}

type sizePricingModel struct {
	BillingCycle    types.String `tfsdk:"billing_cycle"`
	Price           types.String `tfsdk:"price"`
	DiscountPercent types.String `tfsdk:"discount_percent"`
	IsDefault       types.Bool   `tfsdk:"is_default"`
}

type sizeAddonModel struct {
	ID          types.String `tfsdk:"id"`
	Slug        types.String `tfsdk:"slug"`
	Name        types.String `tfsdk:"name"`
	BillingType types.String `tfsdk:"billing_type"`
	IsIncluded  types.Bool   `tfsdk:"is_included"`
}

var (
	sizeVolumeAttrTypes = map[string]attr.Type{
		"id": types.StringType, "volume_type": types.StringType, "disk_gb": types.Int64Type, "is_default": types.BoolType,
	}
	sizePricingAttrTypes = map[string]attr.Type{
		"billing_cycle": types.StringType, "price": types.StringType, "discount_percent": types.StringType, "is_default": types.BoolType,
	}
	sizeAddonAttrTypes = map[string]attr.Type{
		"id": types.StringType, "slug": types.StringType, "name": types.StringType, "billing_type": types.StringType,
		"is_included": types.BoolType,
	}
	instanceSizeAttrTypes = map[string]attr.Type{
		"id": types.StringType, "name": types.StringType, "slug": types.StringType, "cpu_model": types.StringType,
		"vcpu": types.Int64Type, "ram_mb": types.Int64Type, "bandwidth_mbps": types.Int64Type, "description": types.StringType,
		"volumes": types.ListType{ElemType: types.ObjectType{AttrTypes: sizeVolumeAttrTypes}},
		"pricing": types.ListType{ElemType: types.ObjectType{AttrTypes: sizePricingAttrTypes}},
		"addons":  types.ListType{ElemType: types.ObjectType{AttrTypes: sizeAddonAttrTypes}},
	}
)

func newInstanceSizeModel(ctx context.Context, size *client.InstanceSize) (instanceSizeModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	volumes := make([]sizeVolumeModel, 0, len(size.AvailableVolumes))
	for _, v := range size.AvailableVolumes {
		if !v.IsActive {
			continue
		}
		volumes = append(volumes, sizeVolumeModel{
			ID: types.StringValue(v.ID), VolumeType: types.StringValue(v.VolumeType),
			DiskGB: types.Int64Value(v.DiskGB), IsDefault: types.BoolValue(v.IsDefault),
		})
	}
	pricing := make([]sizePricingModel, 0, len(size.PricingOptions))
	for _, p := range size.PricingOptions {
		if !p.IsActive {
			continue
		}
		pricing = append(pricing, sizePricingModel{
			BillingCycle: types.StringValue(p.BillingCycle), Price: types.StringValue(p.Price),
			DiscountPercent: types.StringValue(p.DiscountPercent), IsDefault: types.BoolValue(p.IsDefault),
		})
	}
	addons := make([]sizeAddonModel, 0, len(size.AvailableAddons))
	for _, a := range size.AvailableAddons {
		if !a.IsActive || a.IsHidden || !a.Addon.IsActive {
			continue
		}
		addons = append(addons, sizeAddonModel{
			ID: types.StringValue(a.Addon.ID), Slug: types.StringValue(a.Addon.Slug), Name: types.StringValue(a.Addon.Name),
			BillingType: types.StringValue(a.Addon.BillingType), IsIncluded: types.BoolValue(a.IsIncluded),
		})
	}
	volumeList, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: sizeVolumeAttrTypes}, volumes)
	diags.Append(d...)
	pricingList, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: sizePricingAttrTypes}, pricing)
	diags.Append(d...)
	addonList, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: sizeAddonAttrTypes}, addons)
	diags.Append(d...)
	return instanceSizeModel{
		ID:            types.StringValue(size.ID),
		Name:          types.StringValue(size.Name),
		Slug:          types.StringValue(size.Slug),
		CPUModel:      types.StringValue(size.CPUModel),
		VCPU:          types.Int64Value(size.VCPU),
		RAMMB:         types.Int64Value(size.RAMMB),
		BandwidthMbps: types.Int64Value(size.BandwidthMbps),
		Description:   types.StringValue(size.Description),
		Volumes:       volumeList,
		Pricing:       pricingList,
		Addons:        addonList,
	}, diags
}

func instanceSizeComputedAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name":           schema.StringAttribute{Computed: true, MarkdownDescription: "The display name of the size."},
		"cpu_model":      schema.StringAttribute{Computed: true, MarkdownDescription: "The CPU model of the size, such as `epyc-9554`."},
		"vcpu":           schema.Int64Attribute{Computed: true, MarkdownDescription: "The number of virtual CPUs."},
		"ram_mb":         schema.Int64Attribute{Computed: true, MarkdownDescription: "The memory, in MB."},
		"bandwidth_mbps": schema.Int64Attribute{Computed: true, MarkdownDescription: "The network bandwidth, in Mbit/s."},
		"description":    schema.StringAttribute{Computed: true, MarkdownDescription: "The description of the size (Markdown)."},
		"volumes": schema.ListNestedAttribute{
			Computed: true,
			MarkdownDescription: "The volume configurations the size can be installed on; the instance's `volume_id` " +
				"takes one of their `id`s.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the volume configuration."},
				"volume_type": schema.StringAttribute{Computed: true, MarkdownDescription: "The disk type, such as `nvme` or `ssd`."},
				"disk_gb":     schema.Int64Attribute{Computed: true, MarkdownDescription: "The disk size, in GB."},
				"is_default":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is used when no `volume_id` is given."},
			}},
		},
		"pricing": schema.ListNestedAttribute{
			Computed:            true,
			MarkdownDescription: "The price of the size for each billing cycle it is sold with, excluding VAT.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"billing_cycle":    schema.StringAttribute{Computed: true, MarkdownDescription: "The billing cycle, such as `monthly`."},
				"price":            schema.StringAttribute{Computed: true, MarkdownDescription: "The price per cycle, in EUR, as a decimal string."},
				"discount_percent": schema.StringAttribute{Computed: true, MarkdownDescription: "The discount of the cycle, in percent."},
				"is_default":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is the size's default cycle."},
			}},
		},
		"addons": schema.ListNestedAttribute{
			Computed:            true,
			MarkdownDescription: "The add-ons that can be ordered with the size, for the instance's `addon_ids`.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the add-on."},
				"slug":         schema.StringAttribute{Computed: true, MarkdownDescription: "The slug of the add-on."},
				"name":         schema.StringAttribute{Computed: true, MarkdownDescription: "The name of the add-on."},
				"billing_type": schema.StringAttribute{Computed: true, MarkdownDescription: "How the add-on is billed, such as `recurring`."},
				"is_included":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the size includes it at no cost."},
			}},
		},
	}
}

// NewInstanceSizeDataSource returns the onetsolutions_instance_size data source.
func NewInstanceSizeDataSource() datasource.DataSource {
	return &instanceSizeDataSource{}
}

type instanceSizeDataSource struct {
	common.DataSourceBase
}

func (d *instanceSizeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_size"
}

func (d *instanceSizeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an instance size (vCPUs, memory, disks, prices) by `id` or by `slug`.",
		Attributes: common.MergeAttributes(instanceSizeComputedAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the size. Exactly one of `id` and `slug` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The slug of the size, such as `p-4`. Exactly one of `id` and `slug` is required.",
			},
		}),
	}
}

func (d *instanceSizeDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("slug")),
	}
}

func (d *instanceSizeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config struct {
		ID   types.String `tfsdk:"id"`
		Slug types.String `tfsdk:"slug"`
	}
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("id"), &config.ID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("slug"), &config.Slug)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var size *client.InstanceSize
	if !config.ID.IsNull() {
		found, err := c.GetInstanceSize(ctx, config.ID.ValueString())
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the instance size", err)
			return
		}
		size = found
	} else {
		sizes, err := c.ListInstanceSizes(ctx)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the instance sizes", err)
			return
		}
		slug := config.Slug.ValueString()
		found, diags := common.FindOne(sizes,
			func(s client.InstanceSize) bool { return s.Slug == slug },
			func(s client.InstanceSize) string { return s.ID },
			"instance size", fmt.Sprintf("slug %q", slug))
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		size = &found
	}
	model, diags := newInstanceSizeModel(ctx, size)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

// NewInstanceSizesDataSource returns the onetsolutions_instance_sizes data source.
func NewInstanceSizesDataSource() datasource.DataSource {
	return &instanceSizesDataSource{}
}

type instanceSizesDataSource struct {
	common.DataSourceBase
}

type instanceSizesModel struct {
	MinVCPU  types.Int64  `tfsdk:"min_vcpu"`
	MinRAMMB types.Int64  `tfsdk:"min_ram_mb"`
	CPUModel types.String `tfsdk:"cpu_model"`
	Sizes    types.List   `tfsdk:"sizes"`
}

func (d *instanceSizesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_sizes"
}

func (d *instanceSizesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the instance sizes on sale, optionally filtered, smallest first.",
		Attributes: map[string]schema.Attribute{
			"min_vcpu": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Only sizes with at least this many virtual CPUs.",
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"min_ram_mb": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Only sizes with at least this much memory, in MB.",
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"cpu_model": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only sizes of this CPU model, such as `epyc-9554` (case-insensitive).",
			},
			"sizes": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The matching sizes.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: common.MergeAttributes(instanceSizeComputedAttributes(), map[string]schema.Attribute{
						"id":   schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the size."},
						"slug": schema.StringAttribute{Computed: true, MarkdownDescription: "The slug of the size."},
					}),
				},
			},
		},
	}
}

func (d *instanceSizesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config instanceSizesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	sizes, err := c.ListInstanceSizes(ctx)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the instance sizes", err)
		return
	}
	models := make([]instanceSizeModel, 0, len(sizes))
	for i := range sizes {
		size := &sizes[i]
		if !size.IsActive ||
			(!config.MinVCPU.IsNull() && size.VCPU < config.MinVCPU.ValueInt64()) ||
			(!config.MinRAMMB.IsNull() && size.RAMMB < config.MinRAMMB.ValueInt64()) ||
			(!config.CPUModel.IsNull() && !strings.EqualFold(size.CPUModel, config.CPUModel.ValueString())) {
			continue
		}
		model, diags := newInstanceSizeModel(ctx, size)
		resp.Diagnostics.Append(diags...)
		models = append(models, model)
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: instanceSizeAttrTypes}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.Sizes = list
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
