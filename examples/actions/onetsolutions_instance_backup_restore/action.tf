# Replaces the content of the instance's disks with the backup:
#   terraform apply -invoke=action.onetsolutions_instance_backup_restore.web
action "onetsolutions_instance_backup_restore" "web" {
  config {
    instance_id = onetsolutions_instance.web.id
    backup_id   = onetsolutions_instance_backup.before_upgrade.id
  }
}
