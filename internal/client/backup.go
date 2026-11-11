package client

import (
	"context"
	"net/http"
)

// Task types of the backup area and of the instance actions.
const (
	TaskTypeCreateBackup  = "create_backup"
	TaskTypeRestoreBackup = "restore_backup"
	TaskTypeReboot        = "reboot_vps"
	TaskTypeReset         = "reset_vps"
	TaskTypeReinstall     = "reinstall_os"
)

// CodeBackupOptionRequired is the code of the refusal to back up an instance without the backup
// option.
const CodeBackupOptionRequired = "backup_option_required"

// Backup statuses.
const (
	BackupPending   = "pending"
	BackupRunning   = "running"
	BackupCompleted = "completed"
	BackupFailed    = "failed"
	BackupDeleting  = "deleting"
	BackupDeleted   = "deleted"
)

// Backup limits of create_backup.
const (
	BackupNameMaxLength        = 255
	BackupRetentionDaysMin     = 1
	BackupRetentionDaysMax     = 7
	DefaultBackupRetentionDays = 7
)

// Backup is a backup of an instance's disks. size_gb is a decimal served as a string, null
// until the backup is complete.
type Backup struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Status        string  `json:"status"`
	SizeGB        *string `json:"size_gb"`
	RetentionDays int64   `json:"retention_days"`
	ExpiresAt     *string `json:"expires_at"`
	BackupType    string  `json:"backup_type"`
	Created       string  `json:"created"`
	Modified      string  `json:"modified"`
	// TaskID is only set on the answer of create_backup.
	TaskID string `json:"task_id,omitempty"`
}

// BackupCreate is the body of create_backup (InstanceBackupCreate).
type BackupCreate struct {
	Name          string `json:"name"`
	RetentionDays int64  `json:"retention_days"`
}

type backupRestore struct {
	RestoreAsNew bool `json:"restore_as_new"`
}

// InstanceReinstall is the body of reinstall_instance.
type InstanceReinstall struct {
	ImageID              string                 `json:"image_id"`
	VolumeID             *string                `json:"volume_id,omitempty"`
	SSHKeyIDs            []string               `json:"ssh_key_ids,omitempty"`
	EnablePasswordAuth   bool                   `json:"enable_password_auth"`
	AllowPasswordReset   bool                   `json:"allow_password_reset"`
	InstallManagementKey bool                   `json:"install_management_key"`
	OneClickApps         []OneClickAppSelection `json:"one_click_apps"`
	ControlPanel         *string                `json:"control_panel,omitempty"`
}

const backupsPath = computeInstancesPath + "/{instance_id}/backups"

var (
	routeListBackups   = newRoute(http.MethodGet, backupsPath)
	routeCreateBackup  = newRoute(http.MethodPost, backupsPath)
	routeGetBackup     = newRoute(http.MethodGet, backupsPath+"/{id}")
	routeRestoreBackup = newRoute(http.MethodPost, backupsPath+"/{id}/restore")

	routeRebootComputeInstance    = newRoute(http.MethodPost, computeInstancesPath+"/{id}/reboot")
	routeResetComputeInstance     = newRoute(http.MethodPost, computeInstancesPath+"/{id}/reset")
	routeReinstallComputeInstance = newRoute(http.MethodPost, computeInstancesPath+"/{id}/reinstall")
)

// ListBackups lists every backup of an instance, newest first.
func (c *Client) ListBackups(ctx context.Context, scope Scope, instanceID string) ([]Backup, error) {
	return listAll[Backup](ctx, c, routeListBackups, scope.Params("instance_id", instanceID), nil)
}

// CreateBackup requests a backup of an instance; the answer carries the task taking it.
func (c *Client) CreateBackup(ctx context.Context, scope Scope, instanceID string, in BackupCreate) (*Backup, error) {
	var out Backup
	if err := c.do(ctx, routeCreateBackup, scope.Params("instance_id", instanceID), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBackup reads one backup of an instance.
func (c *Client) GetBackup(ctx context.Context, scope Scope, instanceID, id string) (*Backup, error) {
	var out Backup
	if err := c.do(ctx, routeGetBackup, scope.Params("instance_id", instanceID, "id", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RestoreBackup restores an instance from one of its backups, replacing its disks, and returns
// the task doing it.
func (c *Client) RestoreBackup(ctx context.Context, scope Scope, instanceID, id string) (string, error) {
	var ref InstanceTaskRef
	err := c.do(ctx, routeRestoreBackup, scope.Params("instance_id", instanceID, "id", id), backupRestore{}, &ref)
	if err != nil {
		return "", err
	}
	return ref.TaskID, nil
}

// RebootInstance reboots an instance gracefully and returns the task doing it.
func (c *Client) RebootInstance(ctx context.Context, scope Scope, id string) (string, error) {
	return c.powerAction(ctx, routeRebootComputeInstance, scope, id)
}

// ResetInstance power-cycles an instance at once and returns the task doing it.
func (c *Client) ResetInstance(ctx context.Context, scope Scope, id string) (string, error) {
	return c.powerAction(ctx, routeResetComputeInstance, scope, id)
}

// ReinstallInstance erases an instance's system disk, installs an image on it and returns the
// task doing it.
func (c *Client) ReinstallInstance(ctx context.Context, scope Scope, id string, in InstanceReinstall) (string, error) {
	var ref InstanceTaskRef
	if err := c.do(ctx, routeReinstallComputeInstance, scope.Params("id", id), in, &ref); err != nil {
		return "", err
	}
	return ref.TaskID, nil
}
