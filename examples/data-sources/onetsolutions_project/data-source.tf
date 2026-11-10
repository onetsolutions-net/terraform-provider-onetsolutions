data "onetsolutions_project" "production" {
  name = "Production"
}

output "production_project_id" {
  value = data.onetsolutions_project.production.id
}
