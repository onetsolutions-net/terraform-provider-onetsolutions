terraform {
  required_providers {
    onetsolutions = {
      source = "onetsolutions-net/onetsolutions"
    }
  }
}

variable "onetsolutions_api_key" {
  type      = string
  sensitive = true
}

# organization_id and project_id may be omitted when the API key's user belongs to a single
# organization holding a single project. Every setting can come from an environment variable
# instead: ONETSOLUTIONS_API_KEY, ONETSOLUTIONS_API_URL, ONETSOLUTIONS_ORGANIZATION_ID,
# ONETSOLUTIONS_PROJECT_ID, ONETSOLUTIONS_MAX_REQUESTS_PER_SECOND.
provider "onetsolutions" {
  api_key         = var.onetsolutions_api_key
  organization_id = "3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b"
  project_id      = "8b1d4e2f-6c3a-4f9e-8d7b-2a1c0e9f8d7c"
}
