package compute

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*instanceResource)(nil)
	_ resource.ResourceWithImportState = (*instanceResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*instanceResource)(nil)
)

var instanceTimeouts = common.Timeouts{Create: 30 * time.Minute, Update: 30 * time.Minute, Delete: 5 * time.Minute}

const createOnlyEffect = "It takes effect only when the instance is reinstalled or replaced by a new one."

// NewInstanceResource returns the onetsolutions_instance resource.
func NewInstanceResource() resource.Resource {
	return &instanceResource{}
}

type instanceResource struct {
	common.ResourceBase
}

type oneClickAppModel struct {
	Slug       types.String `tfsdk:"slug"`
	Parameters types.Map    `tfsdk:"parameters"`
}

type instanceModel struct {
	ID                     types.String   `tfsdk:"id"`
	OrganizationID         types.String   `tfsdk:"organization_id"`
	ProjectID              types.String   `tfsdk:"project_id"`
	Name                   types.String   `tfsdk:"name"`
	SizeID                 types.String   `tfsdk:"size_id"`
	ImageID                types.String   `tfsdk:"image_id"`
	BillingCycle           types.String   `tfsdk:"billing_cycle"`
	SSHKeyIDs              types.Set      `tfsdk:"ssh_key_ids"`
	VPCID                  types.String   `tfsdk:"vpc_id"`
	Tags                   types.Set      `tfsdk:"tags"`
	VolumeID               types.String   `tfsdk:"volume_id"`
	ControlPanel           types.String   `tfsdk:"control_panel"`
	OneClickApps           types.List     `tfsdk:"one_click_app"`
	AddonIDs               types.Set      `tfsdk:"addon_ids"`
	AcceptedContractIDs    types.Set      `tfsdk:"accepted_contract_ids"`
	InstallManagementKey   types.Bool     `tfsdk:"install_management_key"`
	AllowPasswordReset     types.Bool     `tfsdk:"allow_password_reset"`
	State                  types.String   `tfsdk:"state"`
	AllowStoppingForUpdate types.Bool     `tfsdk:"allow_stopping_for_update"`
	DeletionProtection     types.Bool     `tfsdk:"deletion_protection"`
	IPv6Enabled            types.Bool     `tfsdk:"ipv6_enabled"`
	CancellationReasonCode types.String   `tfsdk:"cancellation_reason_code"`
	CancellationReason     types.String   `tfsdk:"cancellation_reason"`
	OrderID                types.String   `tfsdk:"order_id"`
	CancellationRequested  types.String   `tfsdk:"cancellation_requested_at"`
	BillingEndDate         types.String   `tfsdk:"billing_end_date"`
	Status                 types.String   `tfsdk:"status"`
	PrimaryIPv4            types.String   `tfsdk:"primary_ipv4"`
	PrimaryIPv6            types.String   `tfsdk:"primary_ipv6"`
	IPAddresses            types.List     `tfsdk:"ip_addresses"`
	OSName                 types.String   `tfsdk:"os_name"`
	Volumes                types.List     `tfsdk:"volumes"`
	RecurringAmount        types.String   `tfsdk:"recurring_amount"`
	NextBillingDate        types.String   `tfsdk:"next_billing_date"`
	IsInstalled            types.Bool     `tfsdk:"is_installed"`
	Locked                 types.Bool     `tfsdk:"locked"`
	Created                types.String   `tfsdk:"created"`
	Timeouts               timeouts.Value `tfsdk:"timeouts"`
}

func (m *instanceModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

// fill copies what the API returns into the model. The create-only inputs are not returned and
// keep their value; the inputs with a default that the API does not return (or returns only as
// installed) are filled only when null, which happens right after an import.
func (m *instanceModel) fill(ctx context.Context, instance *client.Instance, scope client.Scope) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(instance.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.Name = types.StringValue(instance.Name)
	if instance.Size != nil {
		m.SizeID = types.StringValue(instance.Size.ID)
	}
	if instance.Image != nil {
		m.ImageID = types.StringValue(instance.Image.ID)
	}
	m.BillingCycle = types.StringValue(instance.BillingCycle)
	m.VPCID = types.StringNull()
	if instance.VPC != nil {
		m.VPCID = types.StringValue(instance.VPC.ID)
	}
	if len(instance.Tags) > 0 || (!m.Tags.IsNull() && !m.Tags.IsUnknown()) {
		tags, d := stringsSet(ctx, instance.Tags)
		diags.Append(d...)
		m.Tags = tags
	} else {
		m.Tags = types.SetNull(types.StringType)
	}
	if state, ok := powerState(instance.VMStatus()); ok {
		m.State = types.StringValue(state)
	} else if m.State.IsNull() || m.State.IsUnknown() {
		m.State = types.StringValue(stateRunning)
	}
	if m.InstallManagementKey.IsNull() || m.InstallManagementKey.IsUnknown() {
		m.InstallManagementKey = types.BoolValue(true)
	}
	if m.AllowPasswordReset.IsNull() || m.AllowPasswordReset.IsUnknown() {
		m.AllowPasswordReset = types.BoolValue(instance.PasswordReset)
	}
	if m.AllowStoppingForUpdate.IsNull() || m.AllowStoppingForUpdate.IsUnknown() {
		m.AllowStoppingForUpdate = types.BoolValue(false)
	}
	if m.DeletionProtection.IsNull() || m.DeletionProtection.IsUnknown() {
		m.DeletionProtection = types.BoolValue(false)
	}
	if m.CancellationReasonCode.IsNull() || m.CancellationReasonCode.IsUnknown() {
		m.CancellationReasonCode = types.StringValue(client.DefaultCancellationReasonCode)
	}
	if instance.OrderID.Present {
		m.OrderID = optionalString(instance.OrderID.Value)
	} else if m.OrderID.IsUnknown() {
		m.OrderID = types.StringNull()
	}
	m.CancellationRequested = optionalString(instance.CancellationRequestedAt)
	m.BillingEndDate = optionalString(instance.BillingEndDate)
	m.Status = types.StringValue(instance.Status)
	m.PrimaryIPv4 = optionalString(instance.PrimaryIPv4)
	m.PrimaryIPv6 = optionalString(instance.PrimaryIPv6)
	m.IPv6Enabled = types.BoolValue(instance.PrimaryIPv6 != nil)
	addresses, d := ipAddressesValue(instance.IPAddresses)
	diags.Append(d...)
	m.IPAddresses = addresses
	m.OSName = types.StringValue(instance.OSName)
	volumes, d := volumesValue(instance.Volumes)
	diags.Append(d...)
	m.Volumes = volumes
	m.RecurringAmount = optionalString(instance.RecurringAmount)
	m.NextBillingDate = optionalString(instance.NextBillingDate)
	m.IsInstalled = types.BoolValue(instance.IsInstalled)
	m.Locked = types.BoolValue(instance.Locked)
	m.Created = types.StringValue(instance.Created)
	return diags
}

// clearUnknown nulls the computed values still unknown, so a partial state can be saved.
func (m *instanceModel) clearUnknown() {
	for _, s := range []*types.String{&m.Status, &m.PrimaryIPv4, &m.PrimaryIPv6, &m.OSName,
		&m.RecurringAmount, &m.NextBillingDate, &m.Created, &m.BillingCycle, &m.State, &m.OrderID,
		&m.CancellationRequested, &m.BillingEndDate} {
		if s.IsUnknown() {
			*s = types.StringNull()
		}
	}
	for _, b := range []*types.Bool{&m.IsInstalled, &m.Locked, &m.IPv6Enabled} {
		if b.IsUnknown() {
			*b = types.BoolNull()
		}
	}
	if m.IPAddresses.IsUnknown() {
		m.IPAddresses = types.ListNull(types.ObjectType{AttrTypes: ipAddressAttrTypes})
	}
	if m.Volumes.IsUnknown() {
		m.Volumes = types.ListNull(types.ObjectType{AttrTypes: volumeAttrTypes})
	}
}

func (r *instanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

func createOnlyNote(what string) string {
	return what + " Applied when the instance is installed only: a later change is stored in the state with a " +
		"plan-time warning and takes effect only on a reinstall or a new instance; it never replaces the instance."
}

func (r *instanceResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	uuidSet := []validator.Set{setvalidator.ValueStringsAre(common.UUID())}
	createOnlySet := []planmodifier.Set{common.CreateOnlyWarn(createOnlyEffect)}
	createOnlyString := []planmodifier.String{common.CreateOnlyWarn(createOnlyEffect)}
	createOnlyBool := []planmodifier.Bool{common.CreateOnlyWarn(createOnlyEffect)}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an OnetSolutions compute instance, a billed order on its billing cycle.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("instance"),
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The hostname of the instance, such as `web-1` or `web-1.example.com`. Can be " +
					"changed in place.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 253),
					stringvalidator.RegexMatches(hostnamePattern, "must be a hostname: letters, digits and hyphens, in dot-separated labels of at most 63 characters that neither start nor end with a hyphen"),
				},
			},
			"size_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The UUID of the size (see `onetsolutions_instance_size`). Can be changed in place " +
					"to a size the instance is eligible to (checked at plan time): the resize is an upgrade order, applied " +
					"once paid, and needs the instance stopped, so it is refused unless `allow_stopping_for_update` is true.",
				Validators: []validator.String{common.UUID()},
			},
			"image_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the image to install (see `onetsolutions_image`). Changing it replaces the instance.",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"billing_cycle": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(defaultBillingCycle),
				MarkdownDescription: "The billing cycle, one of `" + strings.Join(billingCycles, "`, `") + "`. Defaults to " +
					"`" + defaultBillingCycle + "`. `" + cycleHourly + "` exists only on instances already billed hourly, such " +
					"as imported ones: a new instance cannot be ordered hourly. The cycle changes only together with a new " +
					"`size_id`, and only to a longer cycle.",
				Validators: []validator.String{stringvalidator.OneOf(billingCycles...)},
			},
			"ssh_key_ids": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: createOnlyNote("The UUIDs of the SSH keys (see `onetsolutions_ssh_key`) installed for the administrator account."),
				Validators:          uuidSet,
				PlanModifiers:       createOnlySet,
			},
			"vpc_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The UUID of the VPC (see `onetsolutions_vpc`) the instance joins. Can be " +
					"changed or removed in place. On a new instance, the VPC is attached once the instance is installed.",
				Validators: []validator.String{common.UUID()},
			},
			"tags": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Tags, at most 20, unique ignoring case: 1 to 50 letters, digits, spaces, dots, " +
					"hyphens and underscores, starting with a letter or a digit and not ending with a space. Can be changed " +
					"in place. On a new instance, they are set once the instance is installed.",
				Validators: []validator.Set{
					setvalidator.SizeAtMost(maxTags),
					setvalidator.ValueStringsAre(stringvalidator.RegexMatches(tagPattern,
						"must be 1 to 50 letters, digits, spaces, dots, hyphens and underscores, starting with a letter or a "+
							"digit and not ending with a space")),
					uniqueIgnoringCase{},
				},
			},
			"volume_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: createOnlyNote("The UUID of the volume configuration of the size the instance is installed on, instead of the " +
					"size's default. Only for imported instances; cannot be set on a new instance."),
				Validators:    []validator.String{common.UUID()},
				PlanModifiers: createOnlyString,
			},
			"control_panel": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: createOnlyNote("The slug or UUID of a control panel to install after creation (see `onetsolutions_control_panels`)."),
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
				PlanModifiers:       createOnlyString,
			},
			"addon_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: createOnlyNote("Paid add-ons ordered with the instance, at most 20: an add-on id or " +
					"slug of the chosen size. They are billed on the same order."),
				Validators: []validator.Set{
					setvalidator.SizeAtMost(20),
					setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 100)),
				},
				PlanModifiers: createOnlySet,
			},
			"accepted_contract_ids": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: createOnlyNote("The UUIDs of the contract versions accepted with the order."),
				Validators:          uuidSet,
				PlanModifiers:       createOnlySet,
			},
			"install_management_key": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: createOnlyNote("Whether the OnetSolutions support SSH key is installed. Support and the console's tools " +
					"(such as password reset) need it. Defaults to `true`."),
				PlanModifiers: createOnlyBool,
			},
			"allow_password_reset": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: createOnlyNote("Whether the console may change the operating system's password later. Defaults to `true`."),
				PlanModifiers:       createOnlyBool,
			},
			"state": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(stateRunning),
				MarkdownDescription: "The power state, `running` or `stopped`. Defaults to `running`. A new instance " +
					"with `stopped` is stopped once installed. A change starts or stops the instance.",
				Validators: []validator.String{stringvalidator.OneOf(stateRunning, stateStopped)},
			},
			"allow_stopping_for_update": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether the provider may stop a running instance to change its size, then start " +
					"it again. Defaults to `false`: a new `size_id` on a running instance is refused at plan time.",
			},
			"deletion_protection": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying (or replacing) the instance is refused. Defaults to `false`. " +
					"Set it to `false` and apply before destroying a protected instance.",
			},
			"cancellation_reason_code": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(client.DefaultCancellationReasonCode),
				MarkdownDescription: "Why the instance is cancelled when the resource is destroyed or replaced, one of `" +
					strings.Join(client.CancellationReasonCodes, "`, `") + "`. Defaults to `" +
					client.DefaultCancellationReasonCode + "`. Sent only by the destroy: a change is stored without any " +
					"request, so set it and apply before destroying.",
				Validators: []validator.String{stringvalidator.OneOf(client.CancellationReasonCodes...)},
			},
			"cancellation_reason": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: fmt.Sprintf("A sentence of at least %d words explaining the cancellation, sent "+
					"with `cancellation_reason_code` when the resource is destroyed or replaced. When unset, a default "+
					"sentence is recorded. Sent only by the destroy: a change is stored without any request.",
					client.CancellationReasonMinWords),
				Validators: []validator.String{
					stringvalidator.LengthAtMost(client.CancellationReasonMaxLength),
					common.NoSurroundingWhitespace(),
					minWords(client.CancellationReasonMinWords),
				},
			},
			"order_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The UUID of the order the instance was created from (the order's local id), " +
					"null for older instances.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cancellation_requested_at": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "When a cancellation at the end of the billing period was requested (RFC 3339), " +
					"null when none is. Set by a destroy, or by a cancellation requested in the console: the instance " +
					"then stays in the state, unchanged, until it is terminated.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"billing_end_date": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The day a requested cancellation takes effect (RFC 3339): the instance runs and is " +
					"billed until then. Null when no cancellation is requested.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The status of the service: `pending`, `provisioning`, `active`, `suspended`, " +
					"`terminated`, `cancelled` or `error`.",
			},
			"primary_ipv4": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The primary public IPv4 address.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"primary_ipv6": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The primary public IPv6 address, null when IPv6 is not enabled.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ipv6_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether the instance has public IPv6, at no cost. `true` enables it (on a new " +
					"instance, once installed); the operating system picks the address up at its next reboot. IPv6 cannot " +
					"be disabled: `false` on an instance that has it is refused at plan time. When unset, it follows the " +
					"instance.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"ip_addresses": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Every IP address of the instance.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the address."},
					"address":     schema.StringAttribute{Computed: true, MarkdownDescription: "The IP address."},
					"ip_type":     schema.StringAttribute{Computed: true, MarkdownDescription: "`ipv4` or `ipv6`."},
					"is_primary":  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is the primary address of its family."},
					"reverse_dns": schema.StringAttribute{Computed: true, MarkdownDescription: "The reverse DNS name of the address, empty when unset."},
				}},
			},
			"os_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The name of the installed operating system.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"volumes": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The volumes of the instance.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":        schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the volume."},
					"name":      schema.StringAttribute{Computed: true, MarkdownDescription: "The name of the volume."},
					"size_gb":   schema.Int64Attribute{Computed: true, MarkdownDescription: "The size of the volume in GB."},
					"disk_type": schema.StringAttribute{Computed: true, MarkdownDescription: "The disk type: `ssd`, `nvme` or `unknown`."},
				}},
			},
			"recurring_amount": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The amount billed at each renewal, excluding tax, as a decimal string.",
			},
			"next_billing_date": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The next renewal date (RFC 3339), null when none is scheduled.",
			},
			"is_installed": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the operating system is installed.",
			},
			"locked": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether an operation in progress (backup, snapshot, restore…) locks the instance.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the instance was created (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
		Blocks: map[string]schema.Block{
			"one_click_app": schema.ListNestedBlock{
				MarkdownDescription: createOnlyNote("A One Click App installed with the instance; repeat the block for several."),
				Validators:          []validator.List{listvalidator.SizeAtMost(20)},
				PlanModifiers:       []planmodifier.List{common.CreateOnlyWarn(createOnlyEffect)},
				NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
					"slug": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "The slug of the app.",
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
					},
					"parameters": schema.MapAttribute{
						Optional:            true,
						ElementType:         types.StringType,
						MarkdownDescription: "The app's installation parameters.",
					},
				}},
			},
			common.AttrTimeouts: common.TimeoutsBlock(ctx, instanceTimeouts),
		},
	}
}

func (m *instanceModel) createRequest(ctx context.Context) (client.InstanceCreate, diag.Diagnostics) {
	var diags diag.Diagnostics
	in := client.InstanceCreate{
		Name:                 m.Name.ValueString(),
		SizeID:               m.SizeID.ValueString(),
		ImageID:              m.ImageID.ValueString(),
		BillingCycle:         m.BillingCycle.ValueString(),
		VPCID:                m.VPCID.ValueString(),
		VolumeID:             m.VolumeID.ValueString(),
		ControlPanel:         m.ControlPanel.ValueString(),
		InstallManagementKey: m.InstallManagementKey.IsNull() || m.InstallManagementKey.IsUnknown() || m.InstallManagementKey.ValueBool(),
		AllowPasswordReset:   m.AllowPasswordReset.IsNull() || m.AllowPasswordReset.IsUnknown() || m.AllowPasswordReset.ValueBool(),
	}
	for _, field := range []struct {
		set types.Set
		out *[]string
	}{{m.SSHKeyIDs, &in.SSHKeys}, {m.Tags, &in.Tags}, {m.AddonIDs, &in.AddonIDs}, {m.AcceptedContractIDs, &in.AcceptedContractIDs}} {
		items, d := setStrings(ctx, field.set)
		diags.Append(d...)
		*field.out = items
	}
	if !m.OneClickApps.IsNull() && !m.OneClickApps.IsUnknown() {
		var apps []oneClickAppModel
		diags.Append(m.OneClickApps.ElementsAs(ctx, &apps, false)...)
		for _, app := range apps {
			selection := client.OneClickAppSelection{Slug: app.Slug.ValueString()}
			if !app.Parameters.IsNull() && !app.Parameters.IsUnknown() {
				diags.Append(app.Parameters.ElementsAs(ctx, &selection.Parameters, false)...)
			}
			in.OneClickApps = append(in.OneClickApps, selection)
		}
	}
	return in, diags
}

// createFields maps the API's field errors on create to attributes.
var createFields = map[string]path.Path{
	"name": path.Root("name"), "size_id": path.Root("size_id"), "image_id": path.Root("image_id"),
	"billing_cycle": path.Root("billing_cycle"), "ssh_keys": path.Root("ssh_key_ids"),
	"vpc_id": path.Root("vpc_id"), "tags": path.Root("tags"),
	"volume_id": path.Root("volume_id"), "control_panel": path.Root("control_panel"),
	"one_click_apps": path.Root("one_click_app"), "addon_ids": path.Root("addon_ids"),
	"accepted_contract_ids": path.Root("accepted_contract_ids"),
}

func (r *instanceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		warnDestroy(ctx, req, &resp.Diagnostics)
		return
	}
	var plan instanceModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.State.Raw.IsNull() {
		checkOrderable(&plan, &resp.Diagnostics)
		if !resp.Diagnostics.HasError() && r.Data().Ready() {
			r.previewOrder(ctx, &plan, &resp.Diagnostics)
		}
		return
	}
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	warnReplacement(&plan, &state, &resp.Diagnostics)
	if !planIPv6(ctx, &plan, &state, resp) {
		return
	}
	if !checkResizable(&plan, &state, &resp.Diagnostics) || !r.Data().Ready() {
		return
	}
	r.planResize(ctx, &plan, &state, &resp.Diagnostics)
}

// cancellationEffect says what destroying an instance does to its billing, from the state.
func cancellationEffect(state *instanceModel) string {
	if !state.CancellationRequested.IsNull() && !state.CancellationRequested.IsUnknown() {
		return "Its cancellation is already requested: the destroy only removes it from the Terraform state, and it " +
			"keeps running and billing until " + endOfPeriod(state.BillingEndDate) + "."
	}
	return "Destroying it does not delete it: it requests its cancellation at the end of the billing period, and the " +
		"instance keeps running and billing until " + endOfPeriod(state.NextBillingDate) + ", then is terminated with " +
		"its data. Revoking the cancellation in the console before then keeps it."
}

func endOfPeriod(date types.String) string {
	if date.IsNull() || date.IsUnknown() || date.ValueString() == "" {
		return "the end of its billing period"
	}
	return date.ValueString()
}

// warnDestroy says at plan time what a destroy of the instance costs: a cancellation at the end
// of the period, not a termination. A protected instance gets no warning: its destroy fails.
func warnDestroy(ctx context.Context, req resource.ModifyPlanRequest, diags *diag.Diagnostics) {
	if req.State.Raw.IsNull() {
		return
	}
	var state instanceModel
	if d := req.State.Get(ctx, &state); d.HasError() || state.DeletionProtection.ValueBool() {
		return
	}
	diags.AddWarning("This plan cancels a billed instance",
		fmt.Sprintf("Instance %s (%s) leaves the Terraform state. %s", state.ID.ValueString(), state.Name.ValueString(),
			cancellationEffect(&state)))
}

// warnReplacement says that replacing the instance orders a new one while the old one stays
// billed until the end of its period.
func warnReplacement(plan, state *instanceModel, diags *diag.Diagnostics) {
	imageChanged := common.Known(plan.ImageID) && !plan.ImageID.Equal(state.ImageID)
	if !imageChanged || state.DeletionProtection.ValueBool() {
		return
	}
	diags.AddWarning("Replacing this instance keeps the old one billed",
		fmt.Sprintf("The plan replaces instance %s (%s): it orders a new, billed instance, and the old one is not "+
			"deleted. %s", state.ID.ValueString(), state.Name.ValueString(), cancellationEffect(state)))
}

// planIPv6 refuses at plan time to disable IPv6, which the API cannot do, and plans the IPv6
// address unknown when IPv6 is being enabled. A replacement is a new instance: nothing to refuse.
func planIPv6(ctx context.Context, plan, state *instanceModel, resp *resource.ModifyPlanResponse) bool {
	if !common.Known(plan.IPv6Enabled) || plan.IPv6Enabled.Equal(state.IPv6Enabled) ||
		common.Known(plan.ImageID) && !plan.ImageID.Equal(state.ImageID) {
		return true
	}
	if !plan.IPv6Enabled.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("ipv6_enabled"), "IPv6 cannot be disabled",
			fmt.Sprintf("Instance %s has IPv6 (%s), and it cannot be disabled once enabled. Set ipv6_enabled = true, or "+
				"remove it from the configuration.", state.ID.ValueString(), state.PrimaryIPv6.ValueString()))
		return false
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("primary_ipv6"), types.StringUnknown())...)
	return !resp.Diagnostics.HasError()
}

// checkOrderable refuses at plan time what the API's order refuses or ignores.
func checkOrderable(plan *instanceModel, diags *diag.Diagnostics) {
	if plan.BillingCycle.ValueString() == cycleHourly {
		diags.AddAttributeError(path.Root("billing_cycle"), "Billing cycle not orderable",
			"An instance cannot be ordered on the hourly billing cycle: choose monthly or a longer cycle. hourly is "+
				"accepted only for an instance already billed hourly, such as an imported one.")
	}
	if !plan.VolumeID.IsNull() {
		diags.AddAttributeError(path.Root("volume_id"), "volume_id cannot be set on a new instance",
			"A new instance is installed on the default volume configuration of its size. Remove volume_id: it is "+
				"only kept on imported instances.")
	}
}

// checkResizable refuses at plan time a change of the billing cycle without a new size: the
// API's resize refuses the current size (SAME_SIZE) and offers no other way to change the cycle.
func checkResizable(plan, state *instanceModel, diags *diag.Diagnostics) bool {
	if !common.Known(plan.SizeID, plan.BillingCycle) || plan.BillingCycle.Equal(state.BillingCycle) || !plan.SizeID.Equal(state.SizeID) {
		return true
	}
	diags.AddAttributeError(path.Root("billing_cycle"), "The billing cycle cannot change alone",
		fmt.Sprintf("The API changes the billing cycle of an instance only with a new size: its resize refuses the "+
			"current size. Set billing_cycle back to %q, change it together with size_id, or change it in the "+
			"console; the next refresh then reads the new cycle.", state.BillingCycle.ValueString()))
	return false
}

func formatAmount(value *float64) string {
	if value == nil {
		return "an unknown amount"
	}
	return fmt.Sprintf("%.2f", *value)
}

// previewOrder adds a warning with the price of the order a create places. A failure never
// blocks the plan.
func (r *instanceResource) previewOrder(ctx context.Context, plan *instanceModel, diags *diag.Diagnostics) {
	if !common.Known(plan.OrganizationID, plan.ProjectID, plan.SizeID, plan.ImageID, plan.BillingCycle) {
		return
	}
	in, d := plan.createRequest(ctx)
	if d.HasError() {
		return
	}
	if plan.Name.IsUnknown() {
		in.Name = ""
	}
	preview, err := r.Data().Client.PreviewInstanceOrder(ctx, plan.scope(), in)
	if err != nil {
		tflog.Warn(ctx, "instance order preview failed", map[string]any{"error": err.Error()})
		diags.AddWarning("Could not price the instance order",
			"The plan orders a billed instance, but its price could not be previewed: "+common.ErrorDetail(err))
		return
	}
	detail := fmt.Sprintf("Applying it orders instance %q billed %s %s excluding tax (%s including tax, %s due now) on the "+
		"%s billing cycle. Destroying it later requests its cancellation at the end of the billing period: it is "+
		"billed until then.",
		plan.Name.ValueString(), formatAmount(preview.Total), preview.Currency, formatAmount(preview.TotalInclTax),
		formatAmount(preview.AmountDue), plan.BillingCycle.ValueString())
	if preview.AmountDue != nil && *preview.AmountDue > 0 {
		detail += " The account balance does not cover the amount due: the apply stops once the order is placed, and " +
			"the instance is created only when its invoice is paid in the console (then import it)."
	}
	diags.AddWarning("This plan orders a billed instance", detail)
}

// planResize refuses at plan time a size change that would stop a running instance without
// allow_stopping_for_update, or that targets an ineligible size, and warns with the price of the
// resize (on the new billing cycle when it changes too). API failures never block the plan.
func (r *instanceResource) planResize(ctx context.Context, plan, state *instanceModel, diags *diag.Diagnostics) {
	if !common.Known(plan.SizeID) || plan.SizeID.Equal(state.SizeID) {
		return
	}
	c := r.Data().Client
	scope := state.scope()
	id := state.ID.ValueString()
	if state.State.ValueString() != stateStopped && common.Known(plan.AllowStoppingForUpdate) && !plan.AllowStoppingForUpdate.ValueBool() {
		diags.AddAttributeError(path.Root("size_id"), "Changing the size needs the instance stopped",
			"A new size_id needs the instance stopped, and it is running. Set allow_stopping_for_update = true to let "+
				"the provider stop it, resize it and start it again, or set state = \"stopped\" first.")
		return
	}
	eligible, err := c.ListEligibleInstanceSizes(ctx, scope, id)
	if err != nil {
		diags.AddWarning("Could not check the new size",
			"The sizes this instance can be resized to could not be listed: "+common.ErrorDetail(err))
	} else if !slices.ContainsFunc(eligible, func(s client.InstanceSizeRef) bool { return s.ID == plan.SizeID.ValueString() }) {
		names := make([]string, 0, len(eligible))
		for _, size := range eligible {
			names = append(names, fmt.Sprintf("%s (%s)", size.ID, size.Slug))
		}
		available := "none"
		if len(names) > 0 {
			available = strings.Join(names, ", ")
		}
		diags.AddAttributeError(path.Root("size_id"), "Size not eligible",
			fmt.Sprintf("Instance %s cannot be resized to size %s. Eligible sizes: %s.", id, plan.SizeID.ValueString(), available))
		return
	}
	in := client.InstanceResize{SizeID: plan.SizeID.ValueString()}
	if common.Known(plan.BillingCycle) && !plan.BillingCycle.Equal(state.BillingCycle) {
		in.BillingCycle = plan.BillingCycle.ValueString()
	}
	preview, err := c.PreviewInstanceResize(ctx, scope, id, in)
	if err != nil {
		diags.AddWarning("Could not price the resize",
			"The plan changes the size of a billed instance, but the price could not be previewed: "+common.ErrorDetail(err))
		return
	}
	diags.AddWarning("This plan changes the billing of an instance",
		fmt.Sprintf("Applying it orders a change of instance %s billed %s %s now, then %s %s on the %s billing cycle. "+
			"The change applies once the order is paid.", id, previewAmount(preview.TotalPrice), preview.Currency,
			previewAmount(preview.RecurringPrice), preview.Currency, preview.BillingCycle))
}

func previewAmount(value *string) string {
	if value == nil {
		return "an unknown amount"
	}
	return *value
}

// untaintHint completes an error raised once the instance is installed: Terraform marks it
// tainted, and replacing it would destroy a working, billed server.
const untaintHint = "\n\nThe instance is installed and stays in the state, marked tainted, so the next apply would " +
	"replace it. To keep it, run `terraform untaint` on its address, then apply again."

func (r *instanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := r.ResolveScope(ctx, plan.OrganizationID, plan.ProjectID)
	resp.Diagnostics.Append(diags...)
	in, diags := plan.createRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, instanceTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, localOrderID, ok := r.order(ctx, c, scope, in, &resp.Diagnostics)
	if !ok {
		return
	}

	plan.ID = types.StringValue(id)
	plan.OrderID = types.StringValue(localOrderID)
	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	plan.clearUnknown()
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	instance, err := r.waitReady(ctx, c, scope, id)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not create the instance", err)
		return
	}
	afterInstall := func(summary string, err error) {
		resp.Diagnostics.AddError(summary, common.ErrorDetail(err)+untaintHint)
		if current, getErr := c.GetInstance(context.WithoutCancel(ctx), scope, id); getErr == nil {
			saved := plan
			resp.Diagnostics.Append(saved.fill(ctx, current, scope)...)
			resp.Diagnostics.Append(resp.State.Set(ctx, saved)...)
		}
	}
	if err = r.applyOrderGaps(ctx, c, &plan, instance); err != nil {
		afterInstall("The instance was created but its tags or VPC could not be set", err)
		return
	}
	if plan.IPv6Enabled.ValueBool() {
		if err = r.enableIPv6(ctx, c, scope, id); err != nil {
			afterInstall("The instance was created but IPv6 could not be enabled", err)
			return
		}
	}
	instance, err = r.setPower(ctx, c, scope, id, plan.State.ValueString())
	if err != nil {
		afterInstall("The instance was created but its power state could not be set", err)
		return
	}
	resp.Diagnostics.Append(plan.fill(ctx, instance, scope)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// order places the order and returns the ID of the instance it created and the order's local
// id. The API answers with the order, not the instance: the provider finds the instance by
// listing ?order_id=<local_order_id>, and checks that order_id on each listed instance, so an
// API that ignores the filter never makes it adopt another instance. A request that failed after
// it was sent may have placed the order, with no local id to find it by: the error says so, and
// the provider never retries it, which would order a second, billed instance.
func (r *instanceResource) order(ctx context.Context, c *client.Client, scope client.Scope, in client.InstanceCreate, diags *diag.Diagnostics) (id, localOrderID string, ok bool) {
	order, err := c.CreateInstance(ctx, scope, in)
	if err != nil && orderRefused(err) {
		common.AddClientErrorWithFields(diags, "Could not order the instance", err, createFields)
		return "", "", false
	}
	if err != nil {
		diags.AddError("The instance may have been ordered", fmt.Sprintf(
			"The order request failed after it was sent, so the order may have been placed: %s\n\nNothing was added "+
				"to the Terraform state. Before applying again, check the console: if instance %q was ordered, import "+
				"it (terraform import), or cancel it; otherwise a second instance is ordered.",
			common.ErrorDetail(err), in.Name))
		return "", "", false
	}
	if order.AwaitsPayment() {
		diags.AddError("The instance order awaits payment", awaitingPaymentDetail(order, in.Name))
		return "", "", false
	}
	if order.LocalOrderID == nil || *order.LocalOrderID == "" {
		diags.AddError("The instance was ordered but cannot be found", fmt.Sprintf(
			"Order %s was placed, but the API answered without its local_order_id, so the provider cannot find the "+
				"instance it creates and adopts none. Find instance %q in the console and import it (terraform import), "+
				"or cancel it, before applying again, or a second instance is ordered.", order.OrderNumber, in.Name))
		return "", "", false
	}
	localOrderID = *order.LocalOrderID
	instance, err := r.findOrdered(ctx, c, scope, localOrderID)
	if err != nil {
		diags.AddError("The instance was ordered but could not be found", fmt.Sprintf(
			"%s\n\n%s was placed, but its instance is not in the Terraform state. Once it exists, import it with %s, "+
				"where <address> is this resource's address, or cancel it in the console, before applying again, or a "+
				"second instance is ordered.", common.ErrorDetail(err), orderReference(order), orderImportCommand(localOrderID)))
		return "", "", false
	}
	return instance.ID, localOrderID, true
}

// findOrdered waits for the instance created from the order localOrderID to be listed. An API
// too old to report the order of its instances ends the wait at once.
func (r *instanceResource) findOrdered(ctx context.Context, c *client.Client, scope client.Scope, localOrderID string) (*client.Instance, error) {
	return waiter.Wait[*client.Instance]{
		Description: "the instance of order " + localOrderID + " to appear",
		Pending:     []string{"ordered"},
		Target:      []string{"found"},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.Instance, string, error) {
			instances, err := c.ListInstancesByOrder(ctx, scope, localOrderID)
			if err != nil {
				return nil, "", err
			}
			found, err := orderedInstance(instances, localOrderID)
			if err != nil || found == nil {
				return nil, "ordered", err
			}
			return found, "found", nil
		},
	}.Run(ctx)
}

// applyOrderGaps sets what the order does not apply once the instance is installed: its tags and
// its VPC, through the update path, which waits for the network change.
func (r *instanceResource) applyOrderGaps(ctx context.Context, c *client.Client, plan *instanceModel, instance *client.Instance) error {
	current := *plan
	if diags := current.fill(ctx, instance, plan.scope()); diags.HasError() {
		return fmt.Errorf("reading the instance: %v", diags)
	}
	return r.updateAttributes(ctx, c, plan, &current)
}

const (
	readinessReady      = "ready"
	readinessInstalling = "installing"
)

// waitReady waits until the instance is active, installed and unlocked; status error (or a
// terminated or cancelled instance) ends the wait with an error.
func (r *instanceResource) waitReady(ctx context.Context, c *client.Client, scope client.Scope, id string) (*client.Instance, error) {
	return waiter.Wait[*client.Instance]{
		Description: "instance " + id + " to become active and installed",
		Pending:     []string{readinessInstalling},
		Target:      []string{readinessReady},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.Instance, string, error) {
			instance, err := c.GetInstance(ctx, scope, id)
			if err != nil {
				return nil, "", err
			}
			switch {
			case instance.Status == client.InstanceStatusError || instance.Gone():
				return instance, instance.Status, nil
			case instance.Status == client.InstanceStatusActive && instance.IsInstalled && !instance.Locked:
				return instance, readinessReady, nil
			default:
				return instance, readinessInstalling, nil
			}
		},
	}.Run(ctx)
}

// waitPowerKnown waits until the API reports the instance running or stopped. Right after an
// installation, or while the VM is paused or its state is not synced yet, it reports neither,
// and no power decision can be made from it.
func (r *instanceResource) waitPowerKnown(ctx context.Context, c *client.Client, scope client.Scope, id string) (*client.Instance, string, error) {
	instance, err := waiter.Wait[*client.Instance]{
		Description: "the power state of instance " + id + " to be known (running or stopped)",
		Pending:     []string{"unknown"},
		Target:      []string{"known"},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.Instance, string, error) {
			instance, err := c.GetInstance(ctx, scope, id)
			if err != nil {
				return nil, "", err
			}
			if _, ok := powerState(instance.VMStatus()); ok {
				return instance, "known", nil
			}
			return instance, "unknown", nil
		},
	}.Run(ctx)
	var timeout *waiter.TimeoutError
	if errors.As(err, &timeout) {
		// Not wrapped: the generic timeout advice (taint, replacement) does not fit a wait that
		// changed nothing.
		return nil, "", fmt.Errorf("OnetSolutions has not reported whether instance %s is running or stopped (%s), "+
			"so the provider cannot tell which power action it needs: apply again in a few minutes", id, timeout.Error())
	}
	if err != nil {
		return nil, "", err
	}
	current, _ := powerState(instance.VMStatus())
	return instance, current, nil
}

// alreadyInState reports whether a power action was refused because the API's own power status
// already is the one requested: the desired state holds, and only the synced state lags.
func alreadyInState(err error, want string) bool {
	code := client.CodeInstanceAlreadyRunning
	if want == stateStopped {
		code = client.CodeInstanceAlreadyStopped
	}
	return client.IsConflict(err) && client.HasCode(err, code)
}

// setPower starts or stops the instance unless it already is in that state, and waits for the
// task and for the power state the API reads. It decides only from an observed state: while the
// API reports neither running nor stopped, it waits (bounded by ctx). The API checks the action
// against a power status of its own, which can disagree with the synced state for a moment; its
// "already running/stopped" refusal counts as success, and the wait for the synced state follows.
func (r *instanceResource) setPower(ctx context.Context, c *client.Client, scope client.Scope, id, want string) (*client.Instance, error) {
	instance, current, err := r.waitPowerKnown(ctx, c, scope, id)
	if err != nil {
		return nil, err
	}
	if current == want {
		return instance, nil
	}
	action := c.StartInstance
	if want == stateStopped {
		action = c.StopInstance
	}
	var taskID string
	err = waiter.Retry(ctx, r.Data().Waiter, "changing the power state of instance "+id, func(ctx context.Context) error {
		var actionErr error
		taskID, actionErr = action(ctx, scope, id)
		return actionErr
	}, func(err error) bool { return client.IsConflict(err) && instanceLocked(ctx, c, scope, id) })
	switch {
	case alreadyInState(err, want):
		taskID = ""
	case err != nil:
		return nil, err
	}
	if taskID != "" {
		if _, err := waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, id, taskID); err != nil {
			return nil, err
		}
	}
	return waiter.Wait[*client.Instance]{
		Description: fmt.Sprintf("instance %s to be %s", id, want),
		Target:      []string{want},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.Instance, string, error) {
			instance, err := c.GetInstance(ctx, scope, id)
			if err != nil {
				return nil, "", err
			}
			state, _ := powerState(instance.VMStatus())
			return instance, state, nil
		},
	}.Run(ctx)
}

// instanceLocked reports whether a 409 came from a lock, which ends by itself, rather than from
// a wrong power state.
func instanceLocked(ctx context.Context, c *client.Client, scope client.Scope, id string) bool {
	instance, err := c.GetInstance(ctx, scope, id)
	return err == nil && instance.Locked
}

func (r *instanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	instance, err := c.GetInstance(ctx, state.scope(), state.ID.ValueString())
	if client.IsNotFound(err) || (err == nil && instance.Gone()) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the instance", err)
		return
	}
	resp.Diagnostics.Append(state.fill(ctx, instance, state.scope())...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	if instance.CancellationRequestedAt != nil {
		resp.Diagnostics.AddWarning("The instance's cancellation is requested",
			fmt.Sprintf("Instance %s (%s) has a cancellation requested: it runs and is billed until %s, then it is "+
				"terminated with its data and the next plan orders a new one. It stays in the Terraform state until "+
				"then. To keep it, revoke the cancellation in the console; to let it end, remove it from the "+
				"configuration.", instance.ID, instance.Name, endOfPeriod(optionalString(instance.BillingEndDate))))
	}
}

// restoreTimeout bounds the start that follows a failed resize, which runs even when the update
// timed out: leaving a production server stopped is worse than overrunning the timeout.
const restoreTimeout = 15 * time.Minute

func (r *instanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpUpdate, instanceTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := state.scope()
	id := state.ID.ValueString()

	failed := func(summary string, err error) {
		common.AddClientError(&resp.Diagnostics, summary, err)
		r.saveAfterFailure(ctx, c, &state, &plan, resp)
	}

	if err := r.updateAttributes(ctx, c, &plan, &state); err != nil {
		failed("Could not update the instance", err)
		return
	}
	if plan.IPv6Enabled.ValueBool() && !state.IPv6Enabled.ValueBool() {
		if err := r.enableIPv6(ctx, c, scope, id); err != nil {
			failed("Could not enable IPv6 on the instance", err)
			return
		}
	}
	stopped, err := r.resize(ctx, c, &plan, &state)
	if err != nil {
		if stopped {
			restoreCtx, restoreCancel := context.WithTimeout(context.WithoutCancel(ctx), restoreTimeout)
			if _, startErr := r.setPower(restoreCtx, c, scope, id, plan.State.ValueString()); startErr != nil {
				common.AddClientError(&resp.Diagnostics, "The instance was stopped for the resize and could not be started again", startErr)
			}
			restoreCancel()
		}
		failed("Could not resize the instance", err)
		return
	}
	var instance *client.Instance
	if stopped || !plan.State.Equal(state.State) {
		instance, err = r.setPower(ctx, c, scope, id, plan.State.ValueString())
		if err != nil {
			failed("Could not change the power state of the instance", err)
			return
		}
	} else if instance, err = c.GetInstance(ctx, scope, id); err != nil {
		failed("Could not read the instance", err)
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(plan.fill(ctx, instance, scope)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(common.ClearImported(ctx, resp.Private)...)
}

// saveAfterFailure stores what the API now reports, so a partly applied update is not lost: the
// inputs the API does not return come from the plan, the rest from a fresh read.
func (r *instanceResource) saveAfterFailure(ctx context.Context, c *client.Client, state, plan *instanceModel, resp *resource.UpdateResponse) {
	instance, err := c.GetInstance(context.WithoutCancel(ctx), state.scope(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	merged := *plan
	merged.ID = state.ID
	merged.clearUnknown()
	resp.Diagnostics.Append(merged.fill(ctx, instance, state.scope())...)
	resp.Diagnostics.Append(resp.State.Set(ctx, merged)...)
}

// updateAttributes sends name, tags and vpc_id in one PATCH, then waits for the tasks the API
// queues for a new hostname or VPC.
func (r *instanceResource) updateAttributes(ctx context.Context, c *client.Client, plan, state *instanceModel) error {
	nameChanged := !plan.Name.Equal(state.Name)
	vpcChanged := !plan.VPCID.Equal(state.VPCID)
	tagsChanged := !plan.Tags.Equal(state.Tags)
	if !nameChanged && !vpcChanged && !tagsChanged {
		return nil
	}
	scope := state.scope()
	id := state.ID.ValueString()
	var in client.InstanceUpdate
	if nameChanged {
		name := plan.Name.ValueString()
		in.Name = &name
	}
	if tagsChanged {
		tags, diags := setStrings(ctx, plan.Tags)
		if diags.HasError() {
			return fmt.Errorf("reading tags: %v", diags)
		}
		if tags == nil {
			tags = []string{}
		}
		in.Tags = &tags
	}
	if vpcChanged {
		if plan.VPCID.IsNull() {
			in.DetachVPC = true
		} else {
			vpcID := plan.VPCID.ValueString()
			in.VPCID = &vpcID
		}
	}

	var before []client.InstanceTask
	if nameChanged || vpcChanged {
		tasks, err := c.ListRecentInstanceTasks(ctx, scope, id)
		if err != nil {
			return err
		}
		before = tasks
	}
	err := waiter.Retry(ctx, r.Data().Waiter, "updating instance "+id, func(ctx context.Context) error {
		_, err := c.UpdateInstance(ctx, scope, id, in)
		return err
	}, client.IsConflict)
	if err != nil {
		return err
	}
	var wanted []string
	if nameChanged {
		wanted = append(wanted, client.TaskTypeChangeHostname)
	}
	if vpcChanged {
		wanted = append(wanted, client.TaskTypeUpdateVPC)
	}
	return r.waitNewTasks(ctx, c, scope, id, before, wanted)
}

// enableIPv6 enables IPv6 and waits for the task configuring it. An instance that already has
// IPv6 counts as enabled.
func (r *instanceResource) enableIPv6(ctx context.Context, c *client.Client, scope client.Scope, id string) error {
	var taskID string
	err := waiter.Retry(ctx, r.Data().Waiter, "enabling IPv6 on instance "+id, func(ctx context.Context) error {
		var enableErr error
		taskID, enableErr = c.EnableInstanceIPv6(ctx, scope, id)
		return enableErr
	}, func(err error) bool {
		return client.IsConflict(err) && !client.HasCode(err, client.CodeIPv6AlreadyEnabled)
	})
	switch {
	case client.HasCode(err, client.CodeIPv6AlreadyEnabled):
		return nil
	case err != nil:
		return err
	}
	_, err = waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, id, taskID)
	return err
}

// waitNewTasks waits for the most recent task of each type that was not listed before.
func (r *instanceResource) waitNewTasks(ctx context.Context, c *client.Client, scope client.Scope, id string, before []client.InstanceTask, taskTypes []string) error {
	if len(taskTypes) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for i := range before {
		seen[before[i].ID] = true
	}
	after, err := c.ListRecentInstanceTasks(ctx, scope, id)
	if err != nil {
		return err
	}
	for _, taskType := range taskTypes {
		for i := range after {
			if seen[after[i].ID] || after[i].TaskType != taskType {
				continue
			}
			if _, err := waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, id, after[i].ID); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

// resize orders a new size, with the new billing cycle when it changes too. The order comes
// first, while the instance still runs, so an order the API refuses (an earlier upgrade still
// unpaid, a size no longer eligible) costs no downtime and leaves the instance as it was. A
// running instance is then stopped (allowed by allow_stopping_for_update, checked at plan time
// against the refreshed state and again here against the live one, which must read running or
// stopped before anything is ordered) until the new size applies. stopped reports whether the instance may have been
// stopped: the caller starts it again, whether the resize succeeded or not.
func (r *instanceResource) resize(ctx context.Context, c *client.Client, plan, state *instanceModel) (stopped bool, err error) {
	scope := state.scope()
	id := state.ID.ValueString()
	if plan.SizeID.Equal(state.SizeID) {
		if !plan.BillingCycle.Equal(state.BillingCycle) {
			return false, fmt.Errorf("the billing cycle of instance %s cannot change without a new size_id", id)
		}
		return false, nil
	}
	_, current, err := r.waitPowerKnown(ctx, c, scope, id)
	if err != nil {
		return false, fmt.Errorf("the resize was not ordered: %w", err)
	}
	mustStop := current != stateStopped
	if mustStop && !plan.AllowStoppingForUpdate.ValueBool() {
		return false, fmt.Errorf("instance %s is running, as the API reads it now: set allow_stopping_for_update = true to "+
			"let the provider stop it for the resize; the resize was not ordered", id)
	}
	in := client.InstanceResize{SizeID: plan.SizeID.ValueString()}
	if !plan.BillingCycle.Equal(state.BillingCycle) {
		in.BillingCycle = plan.BillingCycle.ValueString()
	}
	order, err := c.ResizeInstance(ctx, scope, id, in)
	if err != nil {
		return false, err
	}
	if mustStop {
		if _, err = r.setPower(ctx, c, scope, id, stateStopped); err != nil {
			return true, err
		}
	}
	description := fmt.Sprintf("instance %s to have size %s on the %s billing cycle", id, in.SizeID, plan.BillingCycle.ValueString())
	if order.InvoiceURL != nil && *order.InvoiceURL != "" {
		description += " (the resize applies once its invoice is paid: " + *order.InvoiceURL + ")"
	}
	_, err = waiter.Wait[*client.Instance]{
		Description: description,
		Pending:     []string{"ordered"},
		Target:      []string{"applied"},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.Instance, string, error) {
			instance, getErr := c.GetInstance(ctx, scope, id)
			if getErr != nil {
				return nil, "", getErr
			}
			if instance.Size != nil && instance.Size.ID == in.SizeID && instance.BillingCycle == plan.BillingCycle.ValueString() {
				return instance, "applied", nil
			}
			return instance, "ordered", nil
		},
	}.Run(ctx)
	return mustStop, err
}

// Delete requests the cancellation of the instance at the end of its billing period: the API's
// DELETE cancels, it terminates nothing at once, and answers 202 with the instance whether the
// cancellation is new, already requested, or the instance already ended. The resource then
// leaves the state, with a warning saying until when the instance keeps running and billing.
func (r *instanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddError("The instance is protected from deletion",
			fmt.Sprintf("Instance %s has deletion_protection = true. Set it to false and apply before destroying or replacing it.",
				state.ID.ValueString()))
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, instanceTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.InstanceDelete{ReasonCode: state.CancellationReasonCode.ValueString(), Reason: state.CancellationReason.ValueString()}
	if in.ReasonCode == "" {
		in.ReasonCode = client.DefaultCancellationReasonCode
	}
	instance, err := c.DeleteInstance(ctx, state.scope(), state.ID.ValueString(), in)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not request the cancellation of the instance", err,
			map[string]path.Path{"reason_code": path.Root("cancellation_reason_code"), "reason": path.Root("cancellation_reason")})
		return
	}
	resp.Diagnostics.AddWarning("The instance's cancellation is requested", cancellationDetail(instance))
}

// ImportState imports an instance and marks it imported, so its create-only inputs are taken
// from the configuration without a warning until the next apply. The instance is named by its id
// or by the order it was created from ("order:<local_order_id>", what an unpaid order's error
// gives), each optionally after "<organization_id>/<project_id>/".
func (r *instanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var id, organizationID, projectID types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(common.AttrID), &id)...)
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(common.AttrOrganizationID), &organizationID)...)
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(common.AttrProjectID), &projectID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if localOrderID, byOrder := strings.CutPrefix(id.ValueString(), orderImportPrefix); byOrder {
		r.importByOrder(ctx, client.Scope{OrganizationID: organizationID.ValueString(), ProjectID: projectID.ValueString()},
			localOrderID, resp)
	}
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(common.MarkImported(ctx, resp.Private)...)
	}
}

// importByOrder replaces the order named by the import ID with the instance created from it.
func (r *instanceResource) importByOrder(ctx context.Context, scope client.Scope, localOrderID string, resp *resource.ImportStateResponse) {
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !common.IsUUID(localOrderID) {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("%q is not a lowercase UUID: import by order takes %s<local_order_id>.", localOrderID, orderImportPrefix))
		return
	}
	instances, err := c.ListInstancesByOrder(ctx, scope, localOrderID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not find the instance of the order", err)
		return
	}
	instance, err := orderedInstance(instances, localOrderID)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Could not find the instance of the order", err.Error())
	case instance == nil:
		resp.Diagnostics.AddError("The order has no instance yet",
			fmt.Sprintf("No instance of this project was created from order %s yet: the API creates it once the order is "+
				"paid and installs it a few minutes later. Pay its invoice in the console, then import again.", localOrderID))
	default:
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrID), instance.ID)...)
	}
}
