## 0.1.0 (November 10, 2026)

NOTES:

* Initial release. Supports Terraform 1.5+ and OpenTofu 1.6+.
* `onetsolutions_instance` places a billed order. Destroying it requests its cancellation at the
  end of the billing period: the instance keeps running, and billed, until `billing_end_date`.
* When the account balance does not cover an instance order, the apply stops with the invoice to
  pay; once it is paid, import the instance with `order:<local_order_id>`.

FEATURES:

* **New Resource:** `onetsolutions_instance`
* **New Resource:** `onetsolutions_instance_firewall`
* **New Resource:** `onetsolutions_instance_snapshot`
* **New Resource:** `onetsolutions_instance_ip_reverse_dns`
* **New Resource:** `onetsolutions_ssh_key`
* **New Resource:** `onetsolutions_vpc`
* **New Resource:** `onetsolutions_dns_zone`
* **New Resource:** `onetsolutions_dns_record`
* **New Resource:** `onetsolutions_dns_zone_dnssec`
* **New Resource:** `onetsolutions_domain_settings`
* **New Data Source:** `onetsolutions_organization`, `onetsolutions_organizations`
* **New Data Source:** `onetsolutions_project`, `onetsolutions_projects`
* **New Data Source:** `onetsolutions_instance_size`, `onetsolutions_instance_sizes`
* **New Data Source:** `onetsolutions_image`, `onetsolutions_images`
* **New Data Source:** `onetsolutions_control_panels`
* **New Data Source:** `onetsolutions_instance`
* **New Data Source:** `onetsolutions_ssh_key`
* **New Data Source:** `onetsolutions_vpc`
* **New Data Source:** `onetsolutions_dns_zone`
* **New Data Source:** `onetsolutions_domain`
* **New Data Source:** `onetsolutions_hosting_plans`
* **New Data Source:** `onetsolutions_web_hosting`
