resource "onetsolutions_instance_backup" "before_upgrade" {
  instance_id    = onetsolutions_instance.web.id
  name           = "before-php-8-4-upgrade"
  retention_days = 7

  timeouts {
    create = "4h"
  }
}
