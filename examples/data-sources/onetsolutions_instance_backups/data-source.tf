data "onetsolutions_instance_backups" "web" {
  instance_id = onetsolutions_instance.web.id
  status      = "completed"
}

output "latest_restorable_backup" {
  value = try(data.onetsolutions_instance_backups.web.backups[0].id, null)
}
