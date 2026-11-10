# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through GitHub:
[Report a vulnerability](https://github.com/onetsolutions-net/terraform-provider-onetsolutions/security/advisories/new)
(*Security → Advisories → Report a vulnerability*). Include the provider version, what you
observed, and the steps to reproduce it.

We acknowledge every report and keep you informed until a fix is released.

## Scope

This repository covers the Terraform/OpenTofu provider: how it handles your API key, the
requests it sends and what it writes to the state or the logs. A vulnerability in the
OnetSolutions platform or API itself can be reported the same way, or through a ticket from the
OnetSolutions console.

## Supported versions

Fixes are released in the latest minor version of the provider.

## Good practice

- Give the API key through the `ONETSOLUTIONS_API_KEY` environment variable rather than in a
  configuration file; the provider never writes it to its logs.
- Keep your Terraform/OpenTofu state in a backend with encryption and restricted access: like any
  state, it holds the attributes of your resources.
