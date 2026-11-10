terraform {
  required_version = ">= 1.5"

  required_providers {
    onetsolutions = {
      source  = "onetsolutions-net/onetsolutions"
      version = "~> 0.1"
    }
  }
}

# The API key comes from ONETSOLUTIONS_API_KEY; organization_id and project_id from the
# variables, or from the only organization and project of the key's user when left null.
provider "onetsolutions" {
  organization_id = var.organization_id
  project_id      = var.project_id
}
