## 0.2.0 (November 11, 2026)

NOTES:

* Clearer introduction on the provider page.
* `onetsolutions_web_hosting` places a billed order, like `onetsolutions_instance`. Destroying it
  requests its cancellation at the end of the billing period: the web hosting keeps running, and
  billed, until `next_billing_date`. An order the account balance does not cover ends the apply
  with a warning; the web hosting stays in the state and becomes active once the invoice is paid.
* `onetsolutions_domain` registers a domain for one year, billed on order. A registration cannot be
  cancelled or refunded: destroying the resource only removes it from the state.
* `onetsolutions_instance_gpu` is billed by the hour while the GPU is attached. Attaching or
  detaching a GPU restarts the instance and leaves it running.
* `onetsolutions_instance_backup` needs the instance's paid backup option. A backup cannot be
  deleted: destroying the resource only removes it from the state, and the backup expires after
  its retention period.
* Actions need Terraform 1.14 or later. OpenTofu does not run actions; every resource and data
  source works with it as before.
* Hosting redirects and database privileges cannot be removed through the provider: destroying
  them only removes them from the state.
* The documentation's "Catalog & Hosting" section is gone: web hosting pages are listed under
  "Web Hosting", and `onetsolutions_control_panels` under "Compute".

FEATURES:

* **New Resource:** `onetsolutions_instance_gpu`
* **New Resource:** `onetsolutions_instance_backup`
* **New Resource:** `onetsolutions_domain`
* **New Resource:** `onetsolutions_web_hosting`
* **New Resource:** `onetsolutions_hosting_php_version`
* **New Resource:** `onetsolutions_hosting_wordpress`
* **New Resource:** `onetsolutions_hosting_database`
* **New Resource:** `onetsolutions_hosting_database_user`
* **New Resource:** `onetsolutions_hosting_database_privileges`
* **New Resource:** `onetsolutions_hosting_email_account`
* **New Resource:** `onetsolutions_hosting_ftp_account`
* **New Resource:** `onetsolutions_hosting_cron_job`
* **New Resource:** `onetsolutions_hosting_subdomain`
* **New Resource:** `onetsolutions_hosting_addon_domain`
* **New Resource:** `onetsolutions_hosting_redirect`
* **New Data Source:** `onetsolutions_instance_available_gpus`
* **New Data Source:** `onetsolutions_instance_backups`
* **New Data Source:** `onetsolutions_vpc_ip_allocations`
* **New Data Source:** `onetsolutions_domain_availability`
* **New Data Source:** `onetsolutions_tlds`
* **New Data Source:** `onetsolutions_hosting_php_versions`
* **New Data Source:** `onetsolutions_hosting_databases`
* **New Data Source:** `onetsolutions_hosting_database_users`
* **New Data Source:** `onetsolutions_hosting_email_accounts`
* **New Data Source:** `onetsolutions_hosting_ftp_accounts`
* **New Data Source:** `onetsolutions_hosting_cron_jobs`
* **New Data Source:** `onetsolutions_hosting_subdomains`
* **New Data Source:** `onetsolutions_hosting_addon_domains`
* **New Data Source:** `onetsolutions_hosting_redirects`
* **New Data Source:** `onetsolutions_hosting_wordpress_sites`
* **New Action:** `onetsolutions_instance_reboot`
* **New Action:** `onetsolutions_instance_reset`
* **New Action:** `onetsolutions_instance_reinstall`
* **New Action:** `onetsolutions_instance_backup_restore`

ENHANCEMENTS:

* `onetsolutions_instance`: new `ipv6_enabled` argument, which gives the instance a public IPv6
  address. IPv6 cannot be disabled, so `false` on an instance that has it is refused at plan time.

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
