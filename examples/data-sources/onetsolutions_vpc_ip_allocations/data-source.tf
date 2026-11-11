data "onetsolutions_vpc" "backend" {
  name = "backend"
}

data "onetsolutions_vpc_ip_allocations" "backend" {
  vpc_id = data.onetsolutions_vpc.backend.id
}

# The private address of each instance in the VPC, by instance name.
output "backend_private_ips" {
  value = {
    for a in data.onetsolutions_vpc_ip_allocations.backend.allocations : a.instance_name => a.ip_address
    if a.allocation_type == "instance"
  }
}
