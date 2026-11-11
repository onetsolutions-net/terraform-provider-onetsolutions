package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/action/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

const reinstallTimeout = 30 * time.Minute

var (
	_ action.ActionWithConfigure  = (*instanceReinstallAction)(nil)
	_ action.ActionWithModifyPlan = (*instanceReinstallAction)(nil)
)

// NewInstanceReinstallAction returns the onetsolutions_instance_reinstall action.
func NewInstanceReinstallAction() action.Action {
	return &instanceReinstallAction{}
}

type instanceReinstallAction struct {
	actionBase
}

type reinstallModel struct {
	OrganizationID       types.String   `tfsdk:"organization_id"`
	ProjectID            types.String   `tfsdk:"project_id"`
	InstanceID           types.String   `tfsdk:"instance_id"`
	ImageID              types.String   `tfsdk:"image_id"`
	VolumeID             types.String   `tfsdk:"volume_id"`
	SSHKeyIDs            types.Set      `tfsdk:"ssh_key_ids"`
	AllowPasswordReset   types.Bool     `tfsdk:"allow_password_reset"`
	InstallManagementKey types.Bool     `tfsdk:"install_management_key"`
	ControlPanel         types.String   `tfsdk:"control_panel"`
	OneClickApps         []oneClickApp  `tfsdk:"one_click_app"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

type oneClickApp struct {
	Slug       types.String `tfsdk:"slug"`
	Parameters types.Map    `tfsdk:"parameters"`
}

func (m *reinstallModel) target() instanceTarget {
	return instanceTarget{OrganizationID: m.OrganizationID, ProjectID: m.ProjectID, InstanceID: m.InstanceID, Timeouts: m.Timeouts}
}

func (a *instanceReinstallAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_reinstall"
}

func (a *instanceReinstallAction) Schema(ctx context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	blocks := timeoutsBlock(ctx, reinstallTimeout)
	blocks["one_click_app"] = schema.ListNestedBlock{
		MarkdownDescription: "A One Click App installed with the new system; repeat the block for several.",
		Validators:          []validator.List{listvalidator.SizeAtMost(20)},
		NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
			"slug": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The slug of the app.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"parameters": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The installation parameters of the app.",
			},
		}},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reinstalls an instance from an image, erasing its system disk and its snapshots.",
		Attributes: targetAttributes(map[string]schema.Attribute{
			"image_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The UUID of the image to install (see `onetsolutions_image`): the instance's " +
					"current image to start afresh, or another one.",
				Validators: []validator.String{common.UUID()},
			},
			"volume_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The UUID of a volume configuration of the instance's size to install on. When " +
					"unset, the instance keeps its current one.",
				Validators: []validator.String{common.UUID()},
			},
			"ssh_key_ids": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The UUIDs of the SSH keys (see `onetsolutions_ssh_key`) installed for the administrator account.",
				Validators:          []validator.Set{setvalidator.ValueStringsAre(common.UUID())},
			},
			"allow_password_reset": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether the console may change the operating system's password later. Defaults to `true`.",
			},
			"install_management_key": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Whether the OnetSolutions support SSH key is installed. Support and the console's " +
					"tools (such as password reset) need it, and so do One Click Apps and control panels. Defaults to `true`.",
			},
			"control_panel": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "The slug or UUID of a control panel to install (see `onetsolutions_control_panels`).",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
		}),
		Blocks: blocks,
	}
}

func (a *instanceReinstallAction) ModifyPlan(ctx context.Context, req action.ModifyPlanRequest, resp *action.ModifyPlanResponse) {
	var config reinstallModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || !a.data.Ready() || !common.Known(config.InstanceID, config.ImageID) {
		return
	}
	instanceID := config.InstanceID.ValueString()
	resp.Diagnostics.AddWarning("This action erases an instance's data",
		fmt.Sprintf("Invoking onetsolutions_instance_reinstall erases the system disk of instance %s and deletes its "+
			"snapshots. Take a backup first if its data matters.", instanceID))
	if config.OrganizationID.IsUnknown() || config.ProjectID.IsUnknown() {
		return
	}
	scope, diags := a.data.ResolveScope(ctx, config.OrganizationID, config.ProjectID)
	if diags.HasError() {
		return
	}
	instance, err := a.data.Client.GetInstance(ctx, scope, instanceID)
	if err != nil || instance.Image == nil || instance.Image.ID == config.ImageID.ValueString() {
		return
	}
	resp.Diagnostics.AddAttributeWarning(path.Root("image_id"), "Update the instance's image_id too",
		fmt.Sprintf("Instance %s runs image %s; after this reinstall it runs %s. If an onetsolutions_instance "+
			"manages it, set its image_id to %s as well: otherwise the next plan replaces that instance, which "+
			"orders a new one and cancels this one.", instanceID, instance.Image.ID, config.ImageID.ValueString(),
			config.ImageID.ValueString()))
}

func (a *instanceReinstallAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var config reinstallModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, diags := config.request(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	a.run(ctx, config.target(), reinstallTimeout, "reinstall", resp, func(ctx context.Context, c *client.Client, scope client.Scope) (string, error) {
		return c.ReinstallInstance(ctx, scope, config.InstanceID.ValueString(), body)
	})
}

func (m *reinstallModel) request(ctx context.Context) (client.InstanceReinstall, diag.Diagnostics) {
	var diags diag.Diagnostics
	body := client.InstanceReinstall{
		ImageID:              m.ImageID.ValueString(),
		VolumeID:             m.VolumeID.ValueStringPointer(),
		AllowPasswordReset:   boolOr(m.AllowPasswordReset, true),
		InstallManagementKey: boolOr(m.InstallManagementKey, true),
		ControlPanel:         m.ControlPanel.ValueStringPointer(),
		OneClickApps:         []client.OneClickAppSelection{},
	}
	if !m.SSHKeyIDs.IsNull() {
		diags.Append(m.SSHKeyIDs.ElementsAs(ctx, &body.SSHKeyIDs, false)...)
	}
	for _, app := range m.OneClickApps {
		selection := client.OneClickAppSelection{Slug: app.Slug.ValueString()}
		if !app.Parameters.IsNull() {
			diags.Append(app.Parameters.ElementsAs(ctx, &selection.Parameters, false)...)
		}
		body.OneClickApps = append(body.OneClickApps, selection)
	}
	return body, diags
}

func boolOr(value types.Bool, fallback bool) bool {
	if value.IsNull() || value.IsUnknown() {
		return fallback
	}
	return value.ValueBool()
}
