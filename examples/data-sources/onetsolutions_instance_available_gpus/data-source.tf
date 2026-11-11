data "onetsolutions_instance_available_gpus" "web" {
  instance_id = onetsolutions_instance.web.id
}

# The hourly rate of each GPU model that can be attached to the instance now.
output "gpu_hourly_rates" {
  value = { for gpu in data.onetsolutions_instance_available_gpus.web.gpus : gpu.model => gpu.hourly_rate }
}
