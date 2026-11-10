data "onetsolutions_instance_size" "small" {
  slug = "p-4"
}

data "onetsolutions_image" "debian" {
  os_name    = "Debian"
  os_version = "13"
  visibility = "public"
}

resource "onetsolutions_ssh_key" "admin" {
  name       = "admin"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "onetsolutions_vpc" "private" {
  name       = "private"
  cidr_block = "10.0.0.0/16"
}

# Creating this resource places an order billed on billing_cycle. Destroying it requests its
# cancellation at the end of the billing period: it keeps running, and billed, until then.
resource "onetsolutions_instance" "web" {
  name          = "web-1"
  size_id       = data.onetsolutions_instance_size.small.id
  image_id      = data.onetsolutions_image.debian.id
  billing_cycle = "monthly"
  vpc_id        = onetsolutions_vpc.private.id
  tags          = ["web", "production"]

  # Applied at installation only.
  ssh_key_ids = [onetsolutions_ssh_key.admin.id]

  # Let the provider stop and restart the instance when size_id changes.
  allow_stopping_for_update = true
  deletion_protection       = true

  # Sent with the cancellation once deletion_protection is lifted and the resource destroyed.
  cancellation_reason_code = "no_longer_needed"
  cancellation_reason      = "This server is replaced by a larger one next month."

  timeouts {
    create = "45m"
  }
}

output "web_ipv4" {
  value = onetsolutions_instance.web.primary_ipv4
}
