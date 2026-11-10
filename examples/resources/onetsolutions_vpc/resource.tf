resource "onetsolutions_vpc" "backend" {
  name       = "backend"
  cidr_block = "10.0.0.0/16"

  timeouts {
    delete = "20m"
  }
}
