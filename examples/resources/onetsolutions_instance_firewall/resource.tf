# The customer rules of the instance. Platform-managed rules (control-plane access, One Click
# Apps) stay in place and are never listed here.
resource "onetsolutions_instance_firewall" "web" {
  instance_id = onetsolutions_instance.web.id

  rule {
    name             = "ssh-office"
    description      = "SSH from the office only"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    source_cidr      = "198.51.100.0/24"
    destination_port = "22"
    priority         = 100
  }

  rule {
    name             = "web"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    destination_port = "80,443"
    priority         = 200
  }

  rule {
    name      = "no-ping"
    direction = "inbound"
    action    = "drop"
    protocol  = "icmp"
    priority  = 300
  }

  timeouts {
    update = "15m"
  }
}
