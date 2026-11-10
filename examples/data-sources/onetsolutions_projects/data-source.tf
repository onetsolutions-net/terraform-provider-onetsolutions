data "onetsolutions_projects" "all" {}

output "project_ids" {
  value = { for project in data.onetsolutions_projects.all.projects : project.name => project.id }
}
