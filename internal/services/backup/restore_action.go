package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/action/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

const restoreTimeout = 3 * time.Hour

var (
	_ action.ActionWithConfigure  = (*instanceBackupRestoreAction)(nil)
	_ action.ActionWithModifyPlan = (*instanceBackupRestoreAction)(nil)
)

// NewInstanceBackupRestoreAction returns the onetsolutions_instance_backup_restore action.
func NewInstanceBackupRestoreAction() action.Action {
	return &instanceBackupRestoreAction{}
}

type instanceBackupRestoreAction struct {
	actionBase
}

type restoreModel struct {
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	InstanceID     types.String   `tfsdk:"instance_id"`
	BackupID       types.String   `tfsdk:"backup_id"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (a *instanceBackupRestoreAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_backup_restore"
}

func (a *instanceBackupRestoreAction) Schema(ctx context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Restores an instance from one of its backups, replacing the current content of its disks.",
		Attributes: targetAttributes(map[string]schema.Attribute{
			"backup_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The UUID of a completed backup of the instance (see `onetsolutions_instance_backup` " +
					"and `onetsolutions_instance_backups`).",
				Validators: []validator.String{common.UUID()},
			},
		}),
		Blocks: timeoutsBlock(ctx, restoreTimeout),
	}
}

func (a *instanceBackupRestoreAction) ModifyPlan(ctx context.Context, req action.ModifyPlanRequest, resp *action.ModifyPlanResponse) {
	var config restoreModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || !a.data.Ready() || !common.Known(config.InstanceID, config.BackupID) {
		return
	}
	instanceID, backupID := config.InstanceID.ValueString(), config.BackupID.ValueString()
	resp.Diagnostics.AddWarning("This action replaces an instance's data",
		fmt.Sprintf("Invoking onetsolutions_instance_backup_restore stops instance %s, replaces the content of its "+
			"disks with backup %s, and starts it again: everything written since that backup is lost.", instanceID, backupID))
	if config.OrganizationID.IsUnknown() || config.ProjectID.IsUnknown() {
		return
	}
	scope, diags := a.data.ResolveScope(ctx, config.OrganizationID, config.ProjectID)
	if diags.HasError() {
		return
	}
	backup, err := a.data.Client.GetBackup(ctx, scope, instanceID, backupID)
	switch {
	case client.IsNotFound(err):
		resp.Diagnostics.AddAttributeError(path.Root("backup_id"), "Backup not found",
			fmt.Sprintf("Instance %s has no backup %s.", instanceID, backupID))
	case err == nil && backup.Status != client.BackupCompleted:
		resp.Diagnostics.AddAttributeError(path.Root("backup_id"), "The backup cannot be restored",
			fmt.Sprintf("Backup %s (%s) is %s: only a completed backup can be restored.", backup.ID, backup.Name, backup.Status))
	}
}

func (a *instanceBackupRestoreAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var config restoreModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target := instanceTarget{OrganizationID: config.OrganizationID, ProjectID: config.ProjectID, InstanceID: config.InstanceID, Timeouts: config.Timeouts}
	a.run(ctx, target, restoreTimeout, "restore", resp, func(ctx context.Context, c *client.Client, scope client.Scope) (string, error) {
		return c.RestoreBackup(ctx, scope, config.InstanceID.ValueString(), config.BackupID.ValueString())
	})
}
