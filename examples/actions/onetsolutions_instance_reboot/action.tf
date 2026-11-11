action "onetsolutions_instance_reboot" "web" {
  config {
    instance_id = onetsolutions_instance.web.id
  }
}

# Reboot on demand: terraform apply -invoke=action.onetsolutions_instance_reboot.web

# Or reboot the instance whenever its configuration file changes.
resource "terraform_data" "app_config" {
  input = filesha256("${path.module}/app.conf")

  lifecycle {
    action_trigger {
      events  = [after_update]
      actions = [action.onetsolutions_instance_reboot.web]
    }
  }
}
