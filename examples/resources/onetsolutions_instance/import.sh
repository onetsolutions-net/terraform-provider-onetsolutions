# <organization_id>/<project_id>/<id>, or <id> when the provider sets organization_id and project_id.
# The create-only inputs (ssh_key_ids, volume_id, control_panel, one_click_app, addon_ids,
# accepted_contract_ids) are taken from the configuration after an import, without a warning.
terraform import onetsolutions_instance.web 3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b/8b1d4e2f-6c3a-4f9e-8d7b-2a1c0e9f8d7c/9b8a7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d

# An instance whose order was waiting for payment is imported by that order once the invoice is
# paid: order:<local_order_id>, optionally after <organization_id>/<project_id>/.
terraform import onetsolutions_instance.web order:5d4c3b2a-1f0e-4d9c-8b7a-6f5e4d3c2b1a
