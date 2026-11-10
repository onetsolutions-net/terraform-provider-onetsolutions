package compute

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
	_ datasource.DataSourceWithConfigure        = (*instanceDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*instanceDataSource)(nil)
)

// NewInstanceDataSource returns the onetsolutions_instance data source.
func NewInstanceDataSource() datasource.DataSource {
	return &instanceDataSource{}
}

type instanceDataSource struct {
	common.DataSourceBase
}

type instanceDataModel struct {
	ID              types.String `tfsdk:"id"`
	OrganizationID  types.String `tfsdk:"organization_id"`
	ProjectID       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	SizeID          types.String `tfsdk:"size_id"`
	ImageID         types.String `tfsdk:"image_id"`
	BillingCycle    types.String `tfsdk:"billing_cycle"`
	VPCID           types.String `tfsdk:"vpc_id"`
	Tags            types.Set    `tfsdk:"tags"`
	State           types.String `tfsdk:"state"`
	Status          types.String `tfsdk:"status"`
	PrimaryIPv4     types.String `tfsdk:"primary_ipv4"`
	PrimaryIPv6     types.String `tfsdk:"primary_ipv6"`
	IPAddresses     types.List   `tfsdk:"ip_addresses"`
	OSName          types.String `tfsdk:"os_name"`
	Volumes         types.List   `tfsdk:"volumes"`
	RecurringAmount types.String `tfsdk:"recurring_amount"`
	NextBillingDate types.String `tfsdk:"next_billing_date"`
	IsInstalled     types.Bool   `tfsdk:"is_installed"`
	Locked          types.Bool   `tfsdk:"locked"`
	Created         types.String `tfsdk:"created"`
}

func (d *instanceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

func computedString(description string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: description}
}

func (d *instanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a compute instance of a project by `id` or by `name`.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the instance. Exactly one of `id` and `name` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The hostname of the instance. Exactly one of `id` and `name` is required.",
			},
			"size_id":       computedString("The UUID of the size of the instance."),
			"image_id":      computedString("The UUID of the image the instance was installed from."),
			"billing_cycle": computedString("The billing cycle of the instance."),
			"vpc_id":        computedString("The UUID of the VPC the instance joins, null when none."),
			"tags": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The tags of the instance.",
			},
			"state":        computedString("The power state last read: `running` or `stopped`, null while unknown."),
			"status":       computedString("The status of the service: `pending`, `provisioning`, `active`, `suspended`, `terminated`, `cancelled` or `error`."),
			"primary_ipv4": computedString("The primary public IPv4 address."),
			"primary_ipv6": computedString("The primary public IPv6 address, null when IPv6 is not enabled."),
			"ip_addresses": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Every IP address of the instance.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":          computedString("The UUID of the address."),
					"address":     computedString("The IP address."),
					"ip_type":     computedString("`ipv4` or `ipv6`."),
					"is_primary":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is the primary address of its family."},
					"reverse_dns": computedString("The reverse DNS name of the address, empty when unset."),
				}},
			},
			"os_name": computedString("The name of the installed operating system."),
			"volumes": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The volumes of the instance.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":        computedString("The UUID of the volume."),
					"name":      computedString("The name of the volume."),
					"size_gb":   schema.Int64Attribute{Computed: true, MarkdownDescription: "The size of the volume in GB."},
					"disk_type": computedString("The disk type: `ssd`, `nvme` or `unknown`."),
				}},
			},
			"recurring_amount":  computedString("The amount billed at each renewal, excluding tax, as a decimal string."),
			"next_billing_date": computedString("The next renewal date (RFC 3339), null when none is scheduled."),
			"is_installed":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the operating system is installed."},
			"locked":            schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether an operation in progress locks the instance."},
			"created":           computedString("When the instance was created (RFC 3339)."),
		}),
	}
}

func (d *instanceDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *instanceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config instanceDataModel
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

	var instance *client.Instance
	if !config.ID.IsNull() {
		found, err := c.GetInstance(ctx, scope, config.ID.ValueString())
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the instance", err)
			return
		}
		instance = found
	} else {
		instances, err := c.ListActiveInstances(ctx, scope)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the instances", err)
			return
		}
		name := config.Name.ValueString()
		found, findDiags := common.FindOne(instances,
			func(i client.Instance) bool { return i.Name == name },
			func(i client.Instance) string { return i.ID },
			"instance", fmt.Sprintf("name %q", name))
		resp.Diagnostics.Append(findDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		instance = &found
	}

	model := instanceDataModel{
		ID:              types.StringValue(instance.ID),
		OrganizationID:  types.StringValue(scope.OrganizationID),
		ProjectID:       types.StringValue(scope.ProjectID),
		Name:            types.StringValue(instance.Name),
		SizeID:          types.StringNull(),
		ImageID:         types.StringNull(),
		BillingCycle:    types.StringValue(instance.BillingCycle),
		VPCID:           types.StringNull(),
		State:           types.StringNull(),
		Status:          types.StringValue(instance.Status),
		PrimaryIPv4:     optionalString(instance.PrimaryIPv4),
		PrimaryIPv6:     optionalString(instance.PrimaryIPv6),
		OSName:          types.StringValue(instance.OSName),
		RecurringAmount: optionalString(instance.RecurringAmount),
		NextBillingDate: optionalString(instance.NextBillingDate),
		IsInstalled:     types.BoolValue(instance.IsInstalled),
		Locked:          types.BoolValue(instance.Locked),
		Created:         types.StringValue(instance.Created),
	}
	if instance.Size != nil {
		model.SizeID = types.StringValue(instance.Size.ID)
	}
	if instance.Image != nil {
		model.ImageID = types.StringValue(instance.Image.ID)
	}
	if instance.VPC != nil {
		model.VPCID = types.StringValue(instance.VPC.ID)
	}
	if state, ok := powerState(instance.VMStatus()); ok {
		model.State = types.StringValue(state)
	}
	tags, diags := stringsSet(ctx, instance.Tags)
	resp.Diagnostics.Append(diags...)
	model.Tags = tags
	addresses, diags := ipAddressesValue(instance.IPAddresses)
	resp.Diagnostics.Append(diags...)
	model.IPAddresses = addresses
	volumes, diags := volumesValue(instance.Volumes)
	resp.Diagnostics.Append(diags...)
	model.Volumes = volumes
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
