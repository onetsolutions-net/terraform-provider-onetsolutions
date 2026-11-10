package network

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*instanceSnapshotResource)(nil)
	_ resource.ResourceWithImportState = (*instanceSnapshotResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*instanceSnapshotResource)(nil)
)

var snapshotTimeouts = common.Timeouts{Create: 30 * time.Minute, Delete: 15 * time.Minute}

// The API lowercases and trims a snapshot name and then requires this form; the validator
// rejects any other spelling so the state always holds the configured name.
var snapshotNamePattern = regexp.MustCompile(`^[a-z_-][a-z0-9_-]*$`)

// NewInstanceSnapshotResource returns the onetsolutions_instance_snapshot resource.
func NewInstanceSnapshotResource() resource.Resource {
	return &instanceSnapshotResource{}
}

type instanceSnapshotResource struct {
	common.ResourceBase
}

type instanceSnapshotModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	InstanceID     types.String   `tfsdk:"instance_id"`
	Name           types.String   `tfsdk:"name"`
	Description    types.String   `tfsdk:"description"`
	SizeGB         types.String   `tfsdk:"size_gb"`
	Status         types.String   `tfsdk:"status"`
	Created        types.String   `tfsdk:"created"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (m *instanceSnapshotModel) fill(snapshot *client.Snapshot) {
	m.ID = types.StringValue(snapshot.ID)
	m.Name = types.StringValue(snapshot.Name)
	m.Description = types.StringValue(snapshot.Description)
	m.SizeGB = types.StringPointerValue(snapshot.SizeGB)
	m.Status = types.StringValue(snapshot.Status)
	m.Created = types.StringValue(snapshot.Created)
}

func (m *instanceSnapshotModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (r *instanceSnapshotResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_snapshot"
}

func (r *instanceSnapshotResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a snapshot of an instance's disks.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("snapshot"),
			"instance_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the instance to snapshot. Changing it recreates the snapshot.",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The name of the snapshot, unique on the instance: 3 to 255 lowercase letters, digits, " +
					"hyphens and underscores, not starting with a digit. Changing it recreates the snapshot.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 255),
					stringvalidator.RegexMatches(snapshotNamePattern,
						"must be lowercase letters, digits, hyphens and underscores, not starting with a digit"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "A description of the snapshot. Defaults to an empty string; changing it recreates the snapshot.",
				Default:             stringdefault.StaticString(""),
				Validators:          []validator.String{common.NoSurroundingWhitespace()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"size_gb": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The size of the snapshot in GB, a decimal such as `2.35`; null while unknown.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The status of the snapshot: `pending`, `present`, `deleting` or `failed`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the snapshot was requested (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, snapshotTimeouts),
		},
	}
}

func (r *instanceSnapshotResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceSnapshotModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, snapshotTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	instanceID := plan.InstanceID.ValueString()
	var snapshot *client.Snapshot
	err := retryWhileBusy(ctx, r.Data().Waiter, "creating a snapshot of instance "+instanceID, func(ctx context.Context) error {
		var err error
		snapshot, err = c.CreateSnapshot(ctx, scope, instanceID, client.SnapshotCreate{
			Name:        plan.Name.ValueString(),
			Description: plan.Description.ValueString(),
		})
		return err
	})
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the snapshot", err, map[string]path.Path{
			"name": path.Root("name"), "description": path.Root("description"),
		})
		return
	}
	resp.Diagnostics.Append(common.SavePartialState(ctx, &resp.State, snapshot.ID, scope, map[string]attr.Value{
		"instance_id": plan.InstanceID,
		"name":        plan.Name,
		"description": plan.Description,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err = waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, snapshot.TaskID); err != nil {
		common.AddClientError(&resp.Diagnostics, "The snapshot was not taken", err)
		return
	}
	current, err := c.GetSnapshot(ctx, scope, instanceID, snapshot.ID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the new snapshot", err)
		return
	}
	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	plan.fill(current)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *instanceSnapshotResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceSnapshotModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := instanceGone(ctx, c, state.scope(), state.InstanceID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the instance of the snapshot", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	snapshot, err := c.GetSnapshot(ctx, state.scope(), state.InstanceID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the snapshot", err)
		return
	}
	state.fill(snapshot)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called with a change: every input forces a replacement. It only stores a new
// timeouts block.
func (r *instanceSnapshotResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan instanceSnapshotModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *instanceSnapshotResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceSnapshotModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, snapshotTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	scope, instanceID, id := state.scope(), state.InstanceID.ValueString(), state.ID.ValueString()
	var taskID string
	err := retryWhileBusy(ctx, r.Data().Waiter, fmt.Sprintf("deleting snapshot %s", id), func(ctx context.Context) error {
		var err error
		taskID, err = c.DeleteSnapshot(ctx, scope, instanceID, id)
		return err
	})
	if goneWithInstance(ctx, c, scope, instanceID, err) {
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not delete the snapshot", err)
		return
	}
	if _, err := waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, taskID); err != nil {
		common.AddClientError(&resp.Diagnostics, "The snapshot was not deleted", err)
	}
}

func (r *instanceSnapshotResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScopedChild(ctx, req, resp, "instance_id")
}
