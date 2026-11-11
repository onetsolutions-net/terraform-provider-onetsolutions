# Power-cycle an instance that no longer answers:
#   terraform apply -invoke=action.onetsolutions_instance_reset.web
action "onetsolutions_instance_reset" "web" {
  config {
    instance_id = onetsolutions_instance.web.id

    timeouts {
      invoke = "5m"
    }
  }
}
