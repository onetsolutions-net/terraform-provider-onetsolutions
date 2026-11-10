data "onetsolutions_control_panels" "all" {}

output "control_panel_slugs" {
  value = data.onetsolutions_control_panels.all.control_panels[*].slug
}
