resource "onetsolutions_instance_snapshot" "before_upgrade" {
  instance_id = onetsolutions_instance.web.id
  name        = "before-upgrade"
  description = "Taken before the PHP 8.4 upgrade"

  timeouts {
    create = "45m"
  }
}
