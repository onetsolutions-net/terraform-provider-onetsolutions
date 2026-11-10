# <organization_id>/<project_id>/<zone_id>/<record_id>, or <zone_id>/<record_id> when the provider
# sets organization_id and project_id. An import writes the canonical spelling: "@" for the apex,
# names relative to the zone, TXT values unquoted.
terraform import onetsolutions_dns_record.www 3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b/8b1d4e2f-6c3a-4f9e-8d7b-2a1c0e9f8d7c/5e7c2a1b-9d3f-4a6e-8b2c-1d0f9e8a7b6c/9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d
