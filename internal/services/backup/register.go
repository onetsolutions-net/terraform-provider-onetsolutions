// Package backup holds onetsolutions_instance_backup, the onetsolutions_instance_backups data
// source and the instance actions: onetsolutions_instance_reboot, onetsolutions_instance_reset,
// onetsolutions_instance_reinstall and onetsolutions_instance_backup_restore.
package backup

import (
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this area.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{
		NewInstanceBackupResource,
	}
}

// DataSources returns the data sources of this area.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewInstanceBackupsDataSource,
	}
}

// Actions returns the actions of this area.
func Actions() []func() action.Action {
	return []func() action.Action{
		NewInstanceRebootAction,
		NewInstanceResetAction,
		NewInstanceReinstallAction,
		NewInstanceBackupRestoreAction,
	}
}
