variable "organization_id" {
  type        = string
  default     = null
  description = "The organization to create everything in; null picks the only one of the API key's user."
}

variable "project_id" {
  type        = string
  default     = null
  description = "The project to create everything in; null picks the only one of the organization."
}

variable "zone_name" {
  type        = string
  description = "The domain served by the DNS zone, lowercase without a trailing dot, such as example.com."
}

variable "ssh_public_key_path" {
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
  description = "The public key installed on both instances."
}

variable "office_cidr" {
  type        = string
  default     = "198.51.100.0/24"
  description = "The only network allowed to reach the instances over SSH."
}

variable "size_slug" {
  type        = string
  default     = "p-4"
  description = "The size of both instances (see the onetsolutions_instance_sizes data source)."
}
