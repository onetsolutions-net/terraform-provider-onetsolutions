<div align="center">

# OnetSolutions Terraform Provider

Manage your OnetSolutions compute instances, networks, DNS, domains and web hostings as code, with
Terraform or OpenTofu.

[![Terraform Registry](https://img.shields.io/badge/terraform-registry-7B42BC?logo=terraform)](https://registry.terraform.io/providers/onetsolutions-net/onetsolutions/latest)
[![Release](https://img.shields.io/github/v/release/onetsolutions-net/terraform-provider-onetsolutions)](https://github.com/onetsolutions-net/terraform-provider-onetsolutions/releases)
[![Tests](https://img.shields.io/github/actions/workflow/status/onetsolutions-net/terraform-provider-onetsolutions/test.yml?branch=main&label=tests)](https://github.com/onetsolutions-net/terraform-provider-onetsolutions/actions/workflows/test.yml)
[![Terraform](https://img.shields.io/badge/terraform-%E2%89%A5%201.5-844FBA)](https://developer.hashicorp.com/terraform/install)
[![OpenTofu](https://img.shields.io/badge/opentofu-%E2%89%A5%201.6-0D1A2B)](https://opentofu.org/docs/intro/install/)
[![License: MPL-2.0](https://img.shields.io/badge/license-MPL--2.0-blue)](LICENSE)

[Documentation](https://registry.terraform.io/providers/onetsolutions-net/onetsolutions/latest/docs) ·
[OpenTofu Registry](https://search.opentofu.org/provider/onetsolutions-net/onetsolutions) ·
[Examples](examples/complete) ·
[Changelog](CHANGELOG.md) ·
[OnetSolutions](https://onetsolutions.net)

</div>

---

## Quick start

**1. Create an API key** in the OnetSolutions console. It starts with `sk-onetsolutions-` and acts
with your rights, in every organization and project you can reach.

**2. Export it**, so it never lands in a configuration file:

```sh
export ONETSOLUTIONS_API_KEY="sk-onetsolutions-…"
```

**3. Declare the provider** and describe your infrastructure:

```terraform
terraform {
  required_providers {
    onetsolutions = {
      source  = "onetsolutions-net/onetsolutions"
      version = "~> 0.1"
    }
  }
}

provider "onetsolutions" {}

resource "onetsolutions_ssh_key" "laptop" {
  name       = "laptop"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "onetsolutions_vpc" "private" {
  name       = "private"
  cidr_block = "10.0.0.0/16"
}
```

**4. Apply**, with Terraform or OpenTofu — the configuration is the same:

```sh
terraform init && terraform apply     # or: tofu init && tofu apply
```

`organization_id` and `project_id` are optional when your account holds a single organization and
a single project. Otherwise set them on the provider, or on each resource.

> [!IMPORTANT]
> **Some resources are billed.** Creating an `onetsolutions_instance` or an
> `onetsolutions_web_hosting` places an order on its billing cycle (monthly or longer); destroying
> it requests its cancellation at the **end of the billing period**: it keeps running, and billed,
> until then. An `onetsolutions_domain` is registered for a year and cannot be cancelled. A GPU is
> billed by the hour while it is attached, and backups need the instance's paid backup option. The
> plan warns you before anything is ordered or cancelled.

## What you can manage

| Area | Resources | Data sources |
|---|---|---|
| **Compute** | `instance` (with `ipv6_enabled` for a public IPv6 address), `instance_snapshot`, `instance_gpu`, `instance_backup` | `instance`, `instance_size(s)`, `image(s)`, `control_panels`, `instance_available_gpus`, `instance_backups` |
| **Network** | `vpc`, `instance_firewall`, `instance_ip_reverse_dns` | `vpc`, `vpc_ip_allocations` |
| **Access** | `ssh_key` | `ssh_key`, `organization(s)`, `project(s)` |
| **DNS & domains** | `domain` (registration), `domain_settings`, `dns_zone`, `dns_record`, `dns_zone_dnssec` | `domain`, `domain_availability`, `tlds`, `dns_zone` |
| **Web hosting** | `web_hosting`, `hosting_php_version`, `hosting_wordpress`, `hosting_database`, `hosting_database_user`, `hosting_database_privileges`, `hosting_email_account`, `hosting_ftp_account`, `hosting_cron_job`, `hosting_subdomain`, `hosting_addon_domain`, `hosting_redirect` | `web_hosting`, `hosting_plans`, `hosting_php_versions`, `hosting_databases`, `hosting_database_users`, `hosting_email_accounts`, `hosting_ftp_accounts`, `hosting_cron_jobs`, `hosting_subdomains`, `hosting_addon_domains`, `hosting_redirects` |

| Action | What it does |
|---|---|
| `instance_reboot`, `instance_reset` | restart an instance, gracefully or by a hard reset |
| `instance_reinstall` | reinstall an instance from an image, erasing its system disk and snapshots |
| `instance_backup_restore` | restore an instance from one of its backups |

Actions run only when invoked (`terraform apply -invoke=action.onetsolutions_instance_reboot.<name>`)
or from a resource's `action_trigger` block. They need Terraform 1.14 or later; OpenTofu does not
run actions, and everything else works with it.

Every name takes the `onetsolutions_` prefix. Each one has its page, with examples and import
instructions, in the [documentation](https://registry.terraform.io/providers/onetsolutions-net/onetsolutions/latest/docs).

## Configuration

| Setting | Environment variable | Default |
|---|---|---|
| `api_key` | `ONETSOLUTIONS_API_KEY` | required |
| `organization_id` | `ONETSOLUTIONS_ORGANIZATION_ID` | your only organization |
| `project_id` | `ONETSOLUTIONS_PROJECT_ID` | the organization's only project |
| `api_url` | `ONETSOLUTIONS_API_URL` | `https://api.onetsolutions.net` |
| `max_requests_per_second` | `ONETSOLUTIONS_MAX_REQUESTS_PER_SECOND` | `10` |

## A complete example

[`examples/complete`](examples/complete) builds a full stack: a VPC, an SSH key, two instances
with their firewalls, a DNS zone and its records.

## Importing existing resources

Everything you created in the console can be brought under management with an `import` block or
`terraform import` / `tofu import`; each resource page gives its import ID:

```terraform
import {
  to = onetsolutions_ssh_key.laptop
  id = "<organization_id>/<project_id>/<ssh_key_id>"
}
```

If an apply stopped on an unpaid order, pay the invoice, then import the instance by its order:
`terraform import onetsolutions_instance.web order:<local_order_id>`.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) 1.5 or later, or
  [OpenTofu](https://opentofu.org/docs/intro/install/) 1.6 or later. Both install the provider
  from their own registry with the same `source = "onetsolutions-net/onetsolutions"`.
- An OnetSolutions account and an API key.

## Support

- **Account, billing or the API:** open a ticket from the [OnetSolutions console](https://console.onetsolutions.net).
- **Bugs and feature requests for the provider:** [open an issue](https://github.com/onetsolutions-net/terraform-provider-onetsolutions/issues/new/choose).
- **Security problems:** report them privately, as described in [SECURITY.md](SECURITY.md).

## Contributing

Building, testing and writing a resource are described in [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Mozilla Public License 2.0](LICENSE).
