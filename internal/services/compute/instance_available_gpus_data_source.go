package compute

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var _ datasource.DataSourceWithConfigure = (*instanceAvailableGPUsDataSource)(nil)

// NewInstanceAvailableGPUsDataSource returns the onetsolutions_instance_available_gpus data source.
func NewInstanceAvailableGPUsDataSource() datasource.DataSource {
	return &instanceAvailableGPUsDataSource{}
}

type instanceAvailableGPUsDataSource struct {
	common.DataSourceBase
}

type instanceAvailableGPUsModel struct {
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	InstanceID     types.String `tfsdk:"instance_id"`
	GPUs           types.List   `tfsdk:"gpus"`
}

var gpuOptionAttrTypes = map[string]attr.Type{
	"model":                 types.StringType,
	"vendor":                types.StringType,
	"hourly_rate":           types.StringType,
	"vram_gb":               types.Int64Type,
	"memory_type":           types.StringType,
	"memory_bandwidth_gbps": types.Int64Type,
	"architecture":          types.StringType,
	"cuda_cores":            types.Int64Type,
	"tensor_cores":          types.Int64Type,
	"fp16_tflops":           types.Int64Type,
	"fp8_tflops":            types.Int64Type,
	"int8_tops":             types.Int64Type,
	"tdp_watts":             types.Int64Type,
	"pcie":                  types.StringType,
	"form_factor":           types.StringType,
}

func (d *instanceAvailableGPUsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_available_gpus"
}

func (d *instanceAvailableGPUsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	text := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	number := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Computed: true, MarkdownDescription: description + " Null when unknown."}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the GPU models that can be attached to an instance now, one entry per model.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the instance.",
				Validators:          []validator.String{common.UUID()},
			},
			"gpus": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The GPU models free on the instance's host, ordered by model; empty when none is.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"model":                 text("The model, the value of `model` in `onetsolutions_instance_gpu`."),
					"vendor":                text("The vendor, such as `nvidia`."),
					"hourly_rate":           text("The price of an hour of use, as a decimal string."),
					"vram_gb":               number("The memory in GB."),
					"memory_type":           text("The memory type, such as `GDDR6`; empty when unknown."),
					"memory_bandwidth_gbps": number("The memory bandwidth in GB/s."),
					"architecture":          text("The architecture; empty when unknown."),
					"cuda_cores":            number("The number of CUDA cores."),
					"tensor_cores":          number("The number of Tensor cores."),
					"fp16_tflops":           number("The peak FP16 throughput in TFLOPS."),
					"fp8_tflops":            number("The peak FP8 throughput in TFLOPS."),
					"int8_tops":             number("The peak INT8 throughput in TOPS."),
					"tdp_watts":             number("The thermal design power in watts."),
					"pcie":                  text("The PCIe interface; empty when unknown."),
					"form_factor":           text("The form factor; empty when unknown."),
				}},
			},
		}),
	}
}

func gpuOptionValue(g *client.GPUSpecs) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(gpuOptionAttrTypes, map[string]attr.Value{
		"model":                 types.StringValue(g.Model),
		"vendor":                types.StringValue(g.Vendor),
		"hourly_rate":           types.StringValue(g.HourlyRate),
		"vram_gb":               types.Int64PointerValue(g.VRAMGB),
		"memory_type":           types.StringValue(g.MemoryType),
		"memory_bandwidth_gbps": types.Int64PointerValue(g.MemoryBandwidthGBps),
		"architecture":          types.StringValue(g.Architecture),
		"cuda_cores":            types.Int64PointerValue(g.CUDACores),
		"tensor_cores":          types.Int64PointerValue(g.TensorCores),
		"fp16_tflops":           types.Int64PointerValue(g.FP16TFLOPS),
		"fp8_tflops":            types.Int64PointerValue(g.FP8TFLOPS),
		"int8_tops":             types.Int64PointerValue(g.INT8TOPS),
		"tdp_watts":             types.Int64PointerValue(g.TDPWatts),
		"pcie":                  types.StringValue(g.PCIe),
		"form_factor":           types.StringValue(g.FormFactor),
	})
}

func (d *instanceAvailableGPUsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config instanceAvailableGPUsModel
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
	instanceID := config.InstanceID.ValueString()
	available, err := c.ListAvailableGPUs(ctx, scope, instanceID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the GPUs available to the instance", err)
		return
	}
	items := make([]attr.Value, 0, len(available))
	for i := range available {
		value, itemDiags := gpuOptionValue(&available[i])
		resp.Diagnostics.Append(itemDiags...)
		items = append(items, value)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: gpuOptionAttrTypes}, items)
	resp.Diagnostics.Append(diags...)
	config.OrganizationID = types.StringValue(scope.OrganizationID)
	config.ProjectID = types.StringValue(scope.ProjectID)
	config.GPUs = list
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
