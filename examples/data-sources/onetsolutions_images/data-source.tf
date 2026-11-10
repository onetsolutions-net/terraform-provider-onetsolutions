data "onetsolutions_images" "debian" {
  os_name    = "Debian"
  visibility = "public"
}

output "debian_versions" {
  value = data.onetsolutions_images.debian.images[*].os_version
}
