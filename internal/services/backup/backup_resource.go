package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*instanceBackupResource)(nil)
	_ resource.ResourceWithImportState = (*instanceBackupResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*instanceBackupResource)(nil)
)

var backupTimeouts = common.Timeouts{Create: 3 * time.Hour}

// NewInstanceBackupResource returns the onetsolutions_instance_backup resource.
func NewInstanceBackupResource() resource.Resource {
	return &instanceBackupResource{}
}

type instanceBackupResource struct {
	common.ResourceBase
}

type instanceBackupModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	InstanceID     types.String   `tfsdk:"instance_id"`
	Name           types.String   `tfsdk:"name"`
	RetentionDays  types.Int64    `tfsdk:"retention_days"`
	Status         types.String   `tfsdk:"status"`
	SizeGB         types.String   `tfsdk:"size_gb"`
	ExpiresAt      types.String   `tfsdk:"expires_at"`
	BackupType     types.String   `tfsdk:"backup_type"`
	Created        types.String   `tfsdk:"created"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (m *instanceBackupModel) fill(backup *client.Backup) {
	m.ID = types.StringValue(backup.ID)
	m.Name = types.StringValue(backup.Name)
	m.RetentionDays = types.Int64Value(backup.RetentionDays)
	m.Status = types.StringValue(backup.Status)
	m.SizeGB = types.StringPointerValue(backup.SizeGB)
	m.ExpiresAt = types.StringPointerValue(backup.ExpiresAt)
	m.BackupType = types.StringValue(backup.BackupType)
	m.Created = types.StringValue(backup.Created)
}

func (m *instanceBackupModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (r *instanceBackupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_backup"
}

func (r *instanceBackupResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Takes an on-demand backup of an instance's disks, kept for its retention period.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("backup"),
			"instance_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The UUID of the instance to back up; it must have the backup option. Changing it " +
					"takes a new backup.",
				Validators:    []validator.String{common.UUID()},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: fmt.Sprintf("The name of the backup, at most %d characters, unique on the instance "+
					"among all its backups, expired ones included. Changing it takes a new backup.", client.BackupNameMaxLength),
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, client.BackupNameMaxLength),
					common.NoSurroundingWhitespace(),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"retention_days": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(client.DefaultBackupRetentionDays),
				MarkdownDescription: fmt.Sprintf("How many days the backup is kept, from %d to %d. Defaults to %d. "+
					"Changing it takes a new backup, which needs a new `name`.",
					client.BackupRetentionDaysMin, client.BackupRetentionDaysMax, client.DefaultBackupRetentionDays),
				Validators:    []validator.Int64{int64validator.Between(client.BackupRetentionDaysMin, client.BackupRetentionDaysMax)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The status of the backup: `pending`, `running`, `completed`, `failed`, or " +
					"`deleted` once its retention has passed.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"size_gb": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The size of the backup in GB, a decimal such as `3.20`; null until it is complete.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"expires_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the backup expires and is deleted (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"backup_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`manual` for an on-demand backup, `scheduled` for one taken by the backup schedule.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the backup was requested (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, backupTimeouts),
		},
	}
}

func (r *instanceBackupResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		var state instanceBackupModel
		if d := req.State.Get(ctx, &state); !d.HasError() {
			resp.Diagnostics.AddWarning("Destroying this resource keeps the backup", keptBackupMessage(&state))
		}
		return
	}
	var plan instanceBackupModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !req.State.Raw.IsNull() {
		var state instanceBackupModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if !resp.Diagnostics.HasError() {
			checkReplacement(&plan, &state, &resp.Diagnostics)
		}
		return
	}
	r.checkCreate(ctx, &plan, &resp.Diagnostics)
}

// checkReplacement refuses a replacement that would reuse the name of the backup it replaces:
// that backup remains, and names are unique on the instance.
func checkReplacement(plan, state *instanceBackupModel, diags *diag.Diagnostics) {
	if !common.Known(plan.InstanceID, plan.Name, plan.RetentionDays) {
		return
	}
	if !plan.InstanceID.Equal(state.InstanceID) || !plan.Name.Equal(state.Name) {
		diags.AddWarning("Replacing this backup keeps the previous one", keptBackupMessage(state))
		return
	}
	if !plan.RetentionDays.Equal(state.RetentionDays) {
		diags.AddAttributeError(path.Root("retention_days"), "A new backup needs a new name",
			fmt.Sprintf("Changing retention_days takes a new backup, but backup %s keeps the name %q on the instance "+
				"until it expires: set a new name as well.", state.ID.ValueString(), state.Name.ValueString()))
	}
}

// checkCreate refuses at plan time a backup of an instance without the backup option, or under
// a name another backup of the instance already holds.
func (r *instanceBackupResource) checkCreate(ctx context.Context, plan *instanceBackupModel, diags *diag.Diagnostics) {
	data := r.Data()
	if data == nil || !data.Ready() || !common.Known(plan.InstanceID, plan.OrganizationID, plan.ProjectID) {
		return
	}
	scope, instanceID := plan.scope(), plan.InstanceID.ValueString()
	instance, err := data.Client.GetInstance(ctx, scope, instanceID)
	if err != nil {
		return
	}
	if !instance.BackupEnabled {
		diags.AddAttributeError(path.Root("instance_id"), "The instance has no backup option",
			fmt.Sprintf("Instance %s (%s) does not have the backup option, a paid add-on of the instance. Add it in the "+
				"console before taking a backup.", instance.ID, instance.Name))
		return
	}
	if !common.Known(plan.Name) {
		return
	}
	backups, err := data.Client.ListBackups(ctx, scope, instanceID)
	if err != nil {
		return
	}
	for i := range backups {
		if backups[i].Name == plan.Name.ValueString() {
			diags.AddAttributeError(path.Root("name"), "This backup name is taken",
				fmt.Sprintf("Instance %s already has a backup named %q (%s, %s): a name is unique on the instance "+
					"until that backup expires, failed and expired ones included. Choose another name.",
					instanceID, backups[i].Name, backups[i].ID, backups[i].Status))
			return
		}
	}
}

func keptBackupMessage(state *instanceBackupModel) string {
	expiry := "at the end of its retention"
	if !state.ExpiresAt.IsNull() && !state.ExpiresAt.IsUnknown() {
		expiry = "on " + state.ExpiresAt.ValueString()
	}
	return fmt.Sprintf("A backup cannot be deleted: backup %s (%s) leaves the Terraform state, and remains available "+
		"for restores until it expires %s.", state.ID.ValueString(), state.Name.ValueString(), expiry)
}

func (r *instanceBackupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceBackupModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, backupTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	instanceID := plan.InstanceID.ValueString()
	var backup *client.Backup
	err := retryWhileBusy(ctx, r.Data().Waiter, "backing up instance "+instanceID, func(ctx context.Context) error {
		var err error
		backup, err = c.CreateBackup(ctx, scope, instanceID, client.BackupCreate{
			Name:          plan.Name.ValueString(),
			RetentionDays: plan.RetentionDays.ValueInt64(),
		})
		return err
	})
	if err != nil {
		if client.HasCode(err, client.CodeBackupOptionRequired) {
			resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "The instance has no backup option",
				"Instance "+instanceID+" does not have the backup option, a paid add-on of the instance. Add it in the "+
					"console before taking a backup.")
			return
		}
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the backup", err, map[string]path.Path{
			"name": path.Root("name"), "retention_days": path.Root("retention_days"),
		})
		return
	}
	resp.Diagnostics.Append(common.SavePartialState(ctx, &resp.State, backup.ID, scope, map[string]attr.Value{
		"instance_id":    plan.InstanceID,
		"name":           plan.Name,
		"retention_days": plan.RetentionDays,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	waitErr := waitTask(ctx, r.Data().Waiter, c, scope, instanceID, backup.TaskID, nil)
	if waitErr != nil && ctx.Err() != nil {
		common.AddClientError(&resp.Diagnostics, "The backup was not taken", waitErr)
		return
	}
	current, err := c.GetBackup(ctx, scope, instanceID, backup.ID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the new backup", err)
		return
	}
	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	plan.fill(current)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if waitErr != nil {
		common.AddClientError(&resp.Diagnostics, "The backup was not taken", waitErr)
	}
}

func (r *instanceBackupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceBackupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := instanceGone(ctx, c, state.scope(), state.InstanceID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the instance of the backup", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	backup, err := c.GetBackup(ctx, state.scope(), state.InstanceID.ValueString(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the backup", err)
		return
	}
	state.fill(backup)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called with a change: every input forces a replacement. It only stores a new
// timeouts block.
func (r *instanceBackupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan instanceBackupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete only forgets the backup: it cannot be deleted, and expires at the end of its retention.
func (r *instanceBackupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceBackupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning("The backup remains", keptBackupMessage(&state))
}

func (r *instanceBackupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScopedChild(ctx, req, resp, "instance_id")
}
