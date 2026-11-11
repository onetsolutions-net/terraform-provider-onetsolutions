package compute

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*instanceGPUResource)(nil)
	_ resource.ResourceWithImportState = (*instanceGPUResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*instanceGPUResource)(nil)
)

var gpuTimeouts = common.Timeouts{Create: 20 * time.Minute, Delete: 20 * time.Minute}

const gpuRestartNote = "The instance is stopped and started again for the operation, and left running, as when the GPU is " +
	"detached."

// NewInstanceGPUResource returns the onetsolutions_instance_gpu resource.
func NewInstanceGPUResource() resource.Resource {
	return &instanceGPUResource{}
}

type instanceGPUResource struct {
	common.ResourceBase
}

type instanceGPUModel struct {
	ID                 types.String   `tfsdk:"id"`
	OrganizationID     types.String   `tfsdk:"organization_id"`
	ProjectID          types.String   `tfsdk:"project_id"`
	InstanceID         types.String   `tfsdk:"instance_id"`
	Model              types.String   `tfsdk:"model"`
	DeletionProtection types.Bool     `tfsdk:"deletion_protection"`
	Status             types.String   `tfsdk:"status"`
	Vendor             types.String   `tfsdk:"vendor"`
	HourlyRate         types.String   `tfsdk:"hourly_rate"`
	VRAMGB             types.Int64    `tfsdk:"vram_gb"`
	AttachedAt         types.String   `tfsdk:"attached_at"`
	BilledMinutes      types.Int64    `tfsdk:"billed_minutes"`
	Timeouts           timeouts.Value `tfsdk:"timeouts"`
}

func (m *instanceGPUModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (m *instanceGPUModel) fill(attachment *client.GPUAttachment) {
	m.ID = types.StringValue(attachment.ID)
	m.Model = types.StringValue(attachment.Model())
	m.Status = types.StringValue(attachment.Status)
	m.Vendor, m.HourlyRate, m.VRAMGB = types.StringNull(), types.StringNull(), types.Int64Null()
	if attachment.GPU != nil {
		m.Vendor = types.StringValue(attachment.GPU.Vendor)
		m.HourlyRate = types.StringValue(attachment.GPU.HourlyRate)
		m.VRAMGB = types.Int64PointerValue(attachment.GPU.VRAMGB)
	}
	m.AttachedAt = types.StringPointerValue(attachment.AttachedAt)
	m.BilledMinutes = types.Int64Value(attachment.BilledMinutes)
	if m.DeletionProtection.IsNull() || m.DeletionProtection.IsUnknown() {
		m.DeletionProtection = types.BoolValue(false)
	}
}

func (r *instanceGPUResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_gpu"
}

func (r *instanceGPUResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a GPU to an instance, billed by the hour while it stays attached.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("GPU attachment"),
			"instance_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the instance the GPU is attached to. Changing it detaches the GPU and attaches one to the new instance.",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"model": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The GPU model to attach, as `onetsolutions_instance_available_gpus` lists it for the " +
					"instance, such as `NVIDIA L4`. Changing it detaches the GPU and attaches one of the new model.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 255)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"deletion_protection": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether destroying (or replacing) the attachment is refused, so the GPU is not " +
					"released by mistake. Defaults to `false`.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The status of the attachment: `pending`, `active` or `detaching`.",
				PlanModifiers:       keep,
			},
			"vendor": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The vendor of the GPU, such as `nvidia`.",
				PlanModifiers:       keep,
			},
			"hourly_rate": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The price of an hour of the GPU, as a decimal string.",
				PlanModifiers:       keep,
			},
			"vram_gb": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The memory of the GPU in GB, null when unknown.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"attached_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the GPU was attached and its billing started (RFC 3339).",
				PlanModifiers:       keep,
			},
			"billed_minutes": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "The minutes of use billed so far for this attachment, as of the last refresh.",
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, gpuTimeouts),
		},
	}
}

func (r *instanceGPUResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		var state instanceGPUModel
		if d := req.State.Get(ctx, &state); d.HasError() || state.DeletionProtection.ValueBool() {
			return
		}
		resp.Diagnostics.AddWarning("This plan detaches a GPU",
			fmt.Sprintf("GPU %s (%s) is detached from instance %s, which ends its billing. The instance is stopped and "+
				"started again for the operation, and left running.", state.ID.ValueString(), state.Model.ValueString(), state.InstanceID.ValueString()))
		return
	}
	var plan instanceGPUModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !req.State.Raw.IsNull() {
		var state instanceGPUModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() || plan.Model.Equal(state.Model) && plan.InstanceID.Equal(state.InstanceID) ||
			state.DeletionProtection.ValueBool() {
			return
		}
		resp.Diagnostics.AddWarning("Replacing this GPU restarts the instance twice",
			fmt.Sprintf("The plan detaches GPU %s (%s) from instance %s, then attaches a GPU of model %q: each step stops "+
				"and starts the instance it applies to, and the new GPU is billed by the hour from its attachment.",
				state.ID.ValueString(), state.Model.ValueString(), state.InstanceID.ValueString(), plan.Model.ValueString()))
		return
	}
	r.planAttach(ctx, &plan, &resp.Diagnostics)
}

// planAttach refuses at plan time a model the instance's host cannot offer, and warns with the
// price of the attachment. A failure to list the GPUs never blocks the plan.
func (r *instanceGPUResource) planAttach(ctx context.Context, plan *instanceGPUModel, diags *diag.Diagnostics) {
	generic := "Applying it attaches a GPU, billed by the hour while it stays attached. " + gpuRestartNote
	if !r.Data().Ready() || !common.Known(plan.OrganizationID, plan.ProjectID, plan.InstanceID, plan.Model) {
		diags.AddWarning("This plan attaches a billed GPU", generic)
		return
	}
	instanceID, model := plan.InstanceID.ValueString(), plan.Model.ValueString()
	available, err := r.Data().Client.ListAvailableGPUs(ctx, plan.scope(), instanceID)
	if err != nil {
		tflog.Warn(ctx, "listing the available GPUs failed", map[string]any{"error": err.Error()})
		diags.AddWarning("This plan attaches a billed GPU",
			generic+" The GPUs available to the instance could not be listed: "+common.ErrorDetail(err))
		return
	}
	i := slices.IndexFunc(available, func(g client.GPUSpecs) bool { return g.Model == model })
	if i < 0 {
		models := make([]string, 0, len(available))
		for j := range available {
			models = append(models, fmt.Sprintf("%q", available[j].Model))
		}
		listed := "none"
		if len(models) > 0 {
			listed = strings.Join(models, ", ")
		}
		diags.AddAttributeError(path.Root("model"), "GPU model not available",
			fmt.Sprintf("No GPU of model %q can be attached to instance %s now. Models available: %s.", model, instanceID, listed))
		return
	}
	diags.AddWarning("This plan attaches a billed GPU",
		fmt.Sprintf("Applying it attaches a %s GPU to instance %s, billed %s per hour while it stays attached. %s",
			model, instanceID, available[i].HourlyRate, gpuRestartNote))
}

// busyRetry runs op until the instance accepts it, while another task holds the instance or the
// instance is locked. Any other refusal (an instance not installed, already in the state asked)
// is final.
func (r *instanceGPUResource) busyRetry(ctx context.Context, description string, op func(context.Context) error) error {
	return waiter.Retry(ctx, r.Data().Waiter, description, op, instanceBusyWithTask)
}

func instanceBusyWithTask(err error) bool {
	return client.HasCode(err, client.CodeInstanceTaskInProgress) || client.HasCode(err, client.CodeInstanceLocked)
}

func (r *instanceGPUResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceGPUModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, gpuTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	instanceID, model := plan.InstanceID.ValueString(), plan.Model.ValueString()
	before, err := c.ListGPUAttachments(ctx, scope, instanceID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the GPUs of the instance", err)
		return
	}
	var taskID string
	err = r.busyRetry(ctx, "attaching a GPU to instance "+instanceID, func(ctx context.Context) error {
		var attachErr error
		taskID, attachErr = c.AttachGPU(ctx, scope, instanceID, client.GPUAttach{Model: model})
		return attachErr
	})
	var attachment *client.GPUAttachment
	if err != nil {
		attachment = r.attachmentOfLostAnswer(ctx, c, scope, &plan, before, err, &resp.Diagnostics)
		if attachment == nil {
			return
		}
		// The answer naming the task was lost: the task is found by the attachment it carries.
		if taskID, err = c.FindAttachGPUTask(ctx, scope, instanceID, attachment.ID); err != nil {
			tflog.Warn(ctx, "finding the attach task failed", map[string]any{"error": err.Error()})
		}
	} else {
		attachment, err = newAttachment(ctx, c, scope, instanceID, model, before)
		if err == nil && attachment == nil {
			// Not listed yet: the task tells whether it was created at all.
			if _, err = waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, taskID); err == nil {
				attachment, err = newAttachment(ctx, c, scope, instanceID, model, before)
			}
		}
		switch {
		case err != nil:
			common.AddClientError(&resp.Diagnostics, "The GPU attachment was requested but could not be read", err)
			return
		case attachment == nil:
			resp.Diagnostics.AddError("The GPU was attached but cannot be found",
				fmt.Sprintf("Task %s attached a %s GPU to instance %s, but the instance lists no new attachment of that model. "+
					"Check the instance in the console: import the attachment if it exists, before applying again.", taskID, model, instanceID))
			return
		}
	}
	resp.Diagnostics.Append(common.SavePartialState(ctx, &resp.State, attachment.ID, scope, map[string]attr.Value{
		"instance_id":         plan.InstanceID,
		"model":               plan.Model,
		"deletion_protection": plan.DeletionProtection,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	var current *client.GPUAttachment
	if taskID == "" {
		// No task found: the attachment's own status tells when it is done.
		settled := plan
		settled.ID = types.StringValue(attachment.ID)
		settled.OrganizationID = types.StringValue(scope.OrganizationID)
		settled.ProjectID = types.StringValue(scope.ProjectID)
		current, err = r.waitSettled(ctx, c, &settled)
	} else {
		if _, err = waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, taskID); err != nil {
			common.AddClientError(&resp.Diagnostics, "The GPU was not attached", err)
			return
		}
		current, err = findAttachment(ctx, c, scope, instanceID, attachment.ID)
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the GPU attachment", err)
		return
	}
	if current == nil {
		resp.Diagnostics.AddError("The GPU was not attached",
			fmt.Sprintf("The attachment of the %s GPU to instance %s ended before it became active.", model, instanceID))
		return
	}
	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	plan.fill(current)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// attachmentOfLostAnswer handles a failed attach request. A client error means nothing was
// attached. Any other failure (a transport error, a client timeout, a server error) may come
// after the API committed the attachment: it is then returned when the instance lists it, so it is
// tracked rather than left billed outside Terraform. Otherwise the diagnostics say what to check.
func (r *instanceGPUResource) attachmentOfLostAnswer(ctx context.Context, c *client.Client, scope client.Scope, plan *instanceGPUModel,
	before []client.GPUAttachment, err error, diags *diag.Diagnostics) *client.GPUAttachment {
	if orderRefused(err) {
		common.AddClientErrorWithFields(diags, "Could not attach the GPU", err, map[string]path.Path{"model": path.Root("model")})
		return nil
	}
	instanceID, model := plan.InstanceID.ValueString(), plan.Model.ValueString()
	attachment, readErr := newAttachment(ctx, c, scope, instanceID, model, before)
	if readErr == nil && attachment != nil {
		return attachment
	}
	if readErr != nil {
		tflog.Warn(ctx, "listing the GPUs after a failed attach failed", map[string]any{"error": readErr.Error()})
	}
	diags.AddError("The GPU may have been attached", fmt.Sprintf(
		"The attach request failed after it was sent, so a %s GPU may have been attached to instance %s, billed by the "+
			"hour: %s\n\nNothing was added to the Terraform state. Before applying again, check the GPUs of the instance "+
			"in the console: import the attachment if it exists (terraform import), or a second GPU is requested.",
		model, instanceID, common.ErrorDetail(err)))
	return nil
}

// newAttachment returns the attachment of model on the instance that was not listed before, nil
// when there is none yet.
func newAttachment(ctx context.Context, c *client.Client, scope client.Scope, instanceID, model string, before []client.GPUAttachment) (*client.GPUAttachment, error) {
	after, err := c.ListGPUAttachments(ctx, scope, instanceID)
	if err != nil {
		return nil, err
	}
	for i := range after {
		known := slices.ContainsFunc(before, func(a client.GPUAttachment) bool { return a.ID == after[i].ID })
		if !known && after[i].Model() == model {
			return &after[i], nil
		}
	}
	return nil, nil
}

// findAttachment returns an attachment of the instance, nil once it is no longer listed
// (detached or failed).
func findAttachment(ctx context.Context, c *client.Client, scope client.Scope, instanceID, id string) (*client.GPUAttachment, error) {
	attachments, err := c.ListGPUAttachments(ctx, scope, instanceID)
	if err != nil {
		return nil, err
	}
	for i := range attachments {
		if attachments[i].ID == id {
			return &attachments[i], nil
		}
	}
	return nil, nil
}

// readAttachment returns the attachment, nil when it or its instance is gone.
func (r *instanceGPUResource) readAttachment(ctx context.Context, c *client.Client, m *instanceGPUModel) (*client.GPUAttachment, error) {
	gone, err := instanceEnded(ctx, c, m.scope(), m.InstanceID.ValueString())
	if err != nil || gone {
		return nil, err
	}
	attachment, err := findAttachment(ctx, c, m.scope(), m.InstanceID.ValueString(), m.ID.ValueString())
	if client.IsNotFound(err) {
		return nil, nil
	}
	return attachment, err
}

func (r *instanceGPUResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceGPUModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	attachment, err := r.readAttachment(ctx, c, &state)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the GPU attachment", err)
		return
	}
	if attachment == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(attachment)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only stores deletion_protection or a new timeouts block: every other input replaces.
func (r *instanceGPUResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan instanceGPUModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	attachment, err := r.readAttachment(ctx, c, &plan)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the GPU attachment", err)
		return
	}
	if attachment == nil {
		resp.Diagnostics.AddError("The GPU attachment no longer exists",
			fmt.Sprintf("GPU attachment %s was detached outside Terraform. Run a refresh, then apply again to attach a new GPU.", plan.ID.ValueString()))
		return
	}
	plan.fill(attachment)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete detaches the GPU, which ends its billing, and waits until it is detached. An
// attachment still pending is awaited first; one already detaching is only awaited.
func (r *instanceGPUResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceGPUModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.DeletionProtection.ValueBool() {
		resp.Diagnostics.AddError("The GPU attachment is protected from deletion",
			fmt.Sprintf("GPU attachment %s has deletion_protection = true. Set it to false and apply before destroying or replacing it.",
				state.ID.ValueString()))
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, gpuTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, instanceID, id := state.scope(), state.InstanceID.ValueString(), state.ID.ValueString()

	attachment, err := r.waitSettled(ctx, c, &state)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the GPU attachment", err)
		return
	}
	if attachment == nil {
		return
	}
	if attachment.Status == client.GPUAttachmentActive {
		var taskID string
		err = r.busyRetry(ctx, "detaching GPU "+id, func(ctx context.Context) error {
			var detachErr error
			taskID, detachErr = c.DetachGPU(ctx, scope, instanceID, id)
			return detachErr
		})
		if detachedAlready(ctx, c, scope, instanceID, id, err) {
			return
		}
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not detach the GPU", err)
			return
		}
		if _, err := waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, taskID); err != nil {
			common.AddClientError(&resp.Diagnostics, "The GPU was not detached", err)
			return
		}
	}
	if _, err := r.waitGone(ctx, c, &state); err != nil {
		common.AddClientError(&resp.Diagnostics, "The GPU was not detached", err)
	}
}

// detachedAlready reports whether a refused detach means there is nothing left to detach: the
// attachment or its instance is gone, or a change outside Terraform detached it meanwhile.
func detachedAlready(ctx context.Context, c *client.Client, scope client.Scope, instanceID, id string, err error) bool {
	if err == nil {
		return false
	}
	if client.IsNotFound(err) {
		return true
	}
	if gone, readErr := instanceEnded(ctx, c, scope, instanceID); readErr == nil && gone {
		return true
	}
	attachment, readErr := findAttachment(ctx, c, scope, instanceID, id)
	return readErr == nil && attachment == nil
}

const (
	attachmentGone    = "gone"
	attachmentSettled = "settled"
)

// waitSettled waits while the attachment is pending and returns it once active or detaching, nil
// once it is gone.
func (r *instanceGPUResource) waitSettled(ctx context.Context, c *client.Client, m *instanceGPUModel) (*client.GPUAttachment, error) {
	return waiter.Wait[*client.GPUAttachment]{
		Description: "GPU attachment " + m.ID.ValueString() + " to be active",
		Pending:     []string{client.GPUAttachmentPending},
		Target:      []string{attachmentSettled, attachmentGone},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.GPUAttachment, string, error) {
			attachment, err := r.readAttachment(ctx, c, m)
			switch {
			case err != nil:
				return nil, "", err
			case attachment == nil:
				return nil, attachmentGone, nil
			case attachment.Status == client.GPUAttachmentPending:
				return attachment, attachment.Status, nil
			default:
				return attachment, attachmentSettled, nil
			}
		},
	}.Run(ctx)
}

// waitGone waits until the attachment is no longer listed.
func (r *instanceGPUResource) waitGone(ctx context.Context, c *client.Client, m *instanceGPUModel) (*client.GPUAttachment, error) {
	return waiter.Wait[*client.GPUAttachment]{
		Description: "GPU attachment " + m.ID.ValueString() + " to be detached",
		Target:      []string{attachmentGone},
		Config:      r.Data().Waiter,
		Refresh: func(ctx context.Context) (*client.GPUAttachment, string, error) {
			attachment, err := r.readAttachment(ctx, c, m)
			switch {
			case err != nil:
				return nil, "", err
			case attachment == nil:
				return nil, attachmentGone, nil
			default:
				return attachment, attachment.Status, nil
			}
		},
	}.Run(ctx)
}

func (r *instanceGPUResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScopedChild(ctx, req, resp, "instance_id")
}

// instanceEnded reports whether an instance is gone for its owner: not found, cancelled or
// terminated. Its GPU attachments can then no longer be changed, only forgotten.
func instanceEnded(ctx context.Context, c *client.Client, scope client.Scope, instanceID string) (bool, error) {
	instance, err := c.GetInstance(ctx, scope, instanceID)
	if client.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return instance.Gone(), nil
}
