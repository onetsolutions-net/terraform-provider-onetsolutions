data "onetsolutions_instance" "web" {
  name = "web-1"
}

output "web_ipv4" {
  value = data.onetsolutions_instance.web.primary_ipv4
}
