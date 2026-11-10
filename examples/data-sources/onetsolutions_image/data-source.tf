# The public Ubuntu 24.04 image.
data "onetsolutions_image" "ubuntu" {
  os_name    = "Ubuntu"
  os_version = "24.04"
  visibility = "public"
}

# The most recent private Debian 13 image of the project, such as a golden build.
data "onetsolutions_image" "golden" {
  os_name     = "Debian"
  os_version  = "13"
  visibility  = "private"
  most_recent = true
}
