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

# This places an order, billed monthly.
resource "onetsolutions_instance" "web" {
  name          = "web-1"
  size_id       = data.onetsolutions_instance_size.small.id
  image_id      = data.onetsolutions_image.debian.id
  billing_cycle = "monthly"
  vpc_id        = onetsolutions_vpc.private.id
  ssh_key_ids   = [onetsolutions_ssh_key.admin.id]
}

data "onetsolutions_dns_zone" "example" {
  zone_name = "example.com"
}

resource "onetsolutions_dns_record" "web" {
  zone_id = data.onetsolutions_dns_zone.example.id
  type    = "A"
  name    = "www"
  content = onetsolutions_instance.web.primary_ipv4
  ttl     = 300
}
