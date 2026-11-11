data "onetsolutions_instance_available_gpus" "web" {
  instance_id = onetsolutions_instance.web.id
}

# Billed by the hour while attached. Attaching and detaching stop and start the instance.
resource "onetsolutions_instance_gpu" "inference" {
  instance_id = onetsolutions_instance.web.id
  model       = "NVIDIA L4"

  lifecycle {
    precondition {
      condition     = contains(data.onetsolutions_instance_available_gpus.web.gpus[*].model, "NVIDIA L4")
      error_message = "No NVIDIA L4 is free on the host of this instance."
    }
  }
}

output "gpu_hourly_rate" {
  value = onetsolutions_instance_gpu.inference.hourly_rate
}
