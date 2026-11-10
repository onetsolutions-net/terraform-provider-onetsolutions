# Every size with at least 4 vCPUs and 8 GB of memory.
data "onetsolutions_instance_sizes" "medium" {
  min_vcpu   = 4
  min_ram_mb = 8192
}

output "medium_size_slugs" {
  value = data.onetsolutions_instance_sizes.medium.sizes[*].slug
}
