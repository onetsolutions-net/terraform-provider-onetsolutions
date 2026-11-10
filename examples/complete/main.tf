# A complete stack: a private network, an SSH key, two web instances behind the same firewall
# rules, and a DNS zone whose records point at them.
#
# Applying it ORDERS TWO INSTANCES, billed monthly. Destroying the stack cancels them at the end of
# their billing period: they run, and are billed, until then.

data "onetsolutions_instance_size" "web" {
  slug = var.size_slug
}

data "onetsolutions_image" "debian" {
  os_name    = "Debian"
  os_version = "13"
  visibility = "public"
}

resource "onetsolutions_ssh_key" "admin" {
  name       = "admin"
  public_key = file(var.ssh_public_key_path)
}

resource "onetsolutions_vpc" "private" {
  name       = "web-private"
  cidr_block = "10.20.0.0/16"
}

resource "onetsolutions_instance" "web" {
  for_each = toset(["web-1", "web-2"])

  name          = each.key
  size_id       = data.onetsolutions_instance_size.web.id
  image_id      = data.onetsolutions_image.debian.id
  billing_cycle = "monthly"
  vpc_id        = onetsolutions_vpc.private.id
  ssh_key_ids   = [onetsolutions_ssh_key.admin.id]
  tags          = ["web", "terraform"]

  allow_stopping_for_update = true
}

resource "onetsolutions_instance_firewall" "web" {
  for_each = onetsolutions_instance.web

  instance_id = each.value.id

  rule {
    name             = "ssh-office"
    description      = "SSH from the office only"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    source_cidr      = var.office_cidr
    destination_port = "22"
    priority         = 100
  }

  rule {
    name             = "http-https"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    destination_port = "80,443"
    priority         = 200
  }

  rule {
    name        = "vpc"
    direction   = "inbound"
    action      = "accept"
    protocol    = "all"
    source_cidr = onetsolutions_vpc.private.cidr_block
    priority    = 300
  }
}

resource "onetsolutions_dns_zone" "main" {
  zone_name = var.zone_name
}

# One A record per instance, and the apex answering with both.
resource "onetsolutions_dns_record" "instance" {
  for_each = onetsolutions_instance.web

  zone_id = onetsolutions_dns_zone.main.id
  type    = "A"
  name    = each.key
  content = each.value.primary_ipv4
  ttl     = 300
}

resource "onetsolutions_dns_record" "apex" {
  for_each = onetsolutions_instance.web

  zone_id = onetsolutions_dns_zone.main.id
  type    = "A"
  name    = "@"
  content = each.value.primary_ipv4
  ttl     = 300
}

resource "onetsolutions_dns_record" "www" {
  zone_id = onetsolutions_dns_zone.main.id
  type    = "CNAME"
  name    = "www"
  content = var.zone_name
}
