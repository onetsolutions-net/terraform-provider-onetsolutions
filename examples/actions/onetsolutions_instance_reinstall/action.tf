data "onetsolutions_image" "debian" {
  slug = "debian-13"
}

# Erases the system disk of the instance:
#   terraform apply -invoke=action.onetsolutions_instance_reinstall.web
action "onetsolutions_instance_reinstall" "web" {
  config {
    instance_id = onetsolutions_instance.web.id
    image_id    = data.onetsolutions_image.debian.id
    ssh_key_ids = [onetsolutions_ssh_key.deploy.id]
  }
}
