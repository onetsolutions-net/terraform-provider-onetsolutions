---
page_title: "OnetSolutions Provider"
description: |-
  Manage OnetSolutions compute instances, private networks, DNS, domains and web hostings as code.
---

# OnetSolutions Provider

Use the OnetSolutions provider to manage your OnetSolutions infrastructure as code: compute
instances with their snapshots, backups, GPUs and IPv6 addresses, private networks (VPCs) and
firewalls, SSH keys, domain registrations and their settings, DNS zones and records, and web
hostings with their PHP versions, WordPress sites, databases, mailboxes, FTP accounts, cron jobs,
subdomains, addon domains and redirects. Actions reboot, reset, reinstall or restore an instance
on demand (Terraform 1.14 and later).

The provider works with Terraform 1.5 and later and with OpenTofu 1.6 and later, with the same
configuration. It needs an API key, described under [Authentication](#authentication).

~> **Billing.** Creating an instance or a web hosting places an order billed on its billing cycle
(monthly or longer). Destroying one requests its cancellation at the end of the billing period: it
keeps running, and billed, until then. A domain is registered for a year and cannot be cancelled.
A GPU is billed by the hour while it is attached, backups need the instance's paid backup option,
and snapshots beyond an instance's allowance are billed too. The pages of billed resources say so
at the top.

## Authentication

Create an API key in the OnetSolutions console (it starts with `sk-onetsolutions-`) and give it to
the provider through `api_key` or, preferably, the `ONETSOLUTIONS_API_KEY` environment variable,
so it never lands in a configuration file. The key acts with the rights of the user who created
it, in every organization and project that user can reach. The provider never writes it to a log.

Every setting is optional in the configuration, so `terraform validate` works offline; the
provider reports a missing `api_key` when it first needs the API.

## Settings and environment variables

| Setting | Environment variable | Default |
|---|---|---|
| `api_key` | `ONETSOLUTIONS_API_KEY` | — (required) |
| `api_url` | `ONETSOLUTIONS_API_URL` | `https://api.onetsolutions.net` (https only, except `localhost`) |
| `organization_id` | `ONETSOLUTIONS_ORGANIZATION_ID` | the only organization of the key's user |
| `project_id` | `ONETSOLUTIONS_PROJECT_ID` | the only project of the organization |
| `max_requests_per_second` | `ONETSOLUTIONS_MAX_REQUESTS_PER_SECOND` | `10` |

`organization_id` and `project_id` are defaults for the resources that do not set their own. They
apply when a resource is created only: changing them later never moves or recreates an existing
resource. A resource recreates only when its own `organization_id` or `project_id` is set
explicitly to a different value.

## Example Usage

```terraform
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
```

## Quick start

Export the API key, then declare an SSH key, a VPC, an instance on it and a DNS record pointing
at the instance:

```shell
export ONETSOLUTIONS_API_KEY="sk-onetsolutions-..."
```

```terraform
data "onetsolutions_instance_size" "small" {
  slug = "p-4"
}

data "onetsolutions_image" "debian" {
  os_name    = "Debian"
  os_version = "13"
  visibility = "public"
}

resource "onetsolutions_ssh_key" "admin" {
  name       = "admin"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "onetsolutions_vpc" "private" {
  name       = "private"
  cidr_block = "10.0.0.0/16"
}

# This places an order, billed monthly.
resource "onetsolutions_instance" "web" {
  name          = "web-1"
  size_id       = data.onetsolutions_instance_size.small.id
  image_id      = data.onetsolutions_image.debian.id
  billing_cycle = "monthly"
  vpc_id        = onetsolutions_vpc.private.id
  ssh_key_ids   = [onetsolutions_ssh_key.admin.id]
}

data "onetsolutions_dns_zone" "example" {
  zone_name = "example.com"
}

resource "onetsolutions_dns_record" "web" {
  zone_id = data.onetsolutions_dns_zone.example.id
  type    = "A"
  name    = "www"
  content = onetsolutions_instance.web.primary_ipv4
  ttl     = 300
}
```

`terraform plan` shows the price of the instance order as a warning before anything is ordered.
A complete stack — two instances behind a firewall, a DNS zone and its records — is in
[`examples/complete`](https://github.com/onetsolutions-net/terraform-provider-onetsolutions/tree/main/examples/complete).

## Importing

Resources import from `<organization_id>/<project_id>/<id>`, or from the bare `<id>` when the
provider knows the organization and project; child objects (a DNS record, a snapshot, a backup,
a GPU attachment, the reverse DNS of an instance's address, or anything inside a web hosting) from
`<organization_id>/<project_id>/<parent_id>/<id>` or `<parent_id>/<id>`, where the `<id>` of an
object inside a web hosting is usually its name (a database, a mailbox address, a domain); cron
jobs, WordPress sites, redirects and database privileges have their own format. Each resource
page gives its exact import ID. A firewall is the rule set of one instance, so it imports from the
instance's ID: `<organization_id>/<project_id>/<instance_id>` or `<instance_id>`.

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `api_key` (String, Sensitive) An API key created in the OnetSolutions console (`sk-onetsolutions-…`). Required, here or through `ONETSOLUTIONS_API_KEY`.
- `api_url` (String) The API endpoint. Defaults to `https://api.onetsolutions.net`. Can also be set with the `ONETSOLUTIONS_API_URL` environment variable. Must use https, except for `localhost`; the provider never follows a redirect, so the key is sent to this URL only.
- `max_requests_per_second` (Number) The most API requests the provider sends per second, across all resources. Defaults to 10. Can also be set with the `ONETSOLUTIONS_MAX_REQUESTS_PER_SECOND` environment variable.
- `organization_id` (String) The organization resources are created in when they do not set one. Can also be set with the `ONETSOLUTIONS_ORGANIZATION_ID` environment variable. May be omitted when the API key's user belongs to exactly one organization.
- `project_id` (String) The project resources are created in when they do not set one. Can also be set with the `ONETSOLUTIONS_PROJECT_ID` environment variable. May be omitted when the organization has exactly one project.
