# Contributing

Thanks for helping improve the provider. Bug reports and feature requests are welcome as
[issues](https://github.com/onetsolutions-net/terraform-provider-onetsolutions/issues/new/choose);
for a change you would like to make, open an issue first so we can agree on the approach.

Pull requests are welcome too. This repository is published from the maintainers' development
repository at each release, so an accepted pull request is applied there by a maintainer, with
you credited, and reaches this repository with the next release rather than by a merge here.

The provider calls the OnetSolutions public API only: its contract is the snapshot
[`schema/openapi.json`](schema/openapi.json), and a test fails when the client calls an operation
outside it. User documentation is generated into [`docs/`](docs/) for the Terraform Registry.

## Requirements

Go (the version of `go.mod`), Terraform 1.5 or later on `PATH` (the provider tests run the real
binary), `golangci-lint` v2, and GoReleaser only to dry-run a release.

## Build

```sh
make build      # ./terraform-provider-onetsolutions, version "dev"
make install    # into $(go env GOPATH)/bin, for dev_overrides
```

To run Terraform against a local build, point it at the directory holding the binary with a CLI
configuration file — `~/.terraformrc`, or any file named by `TF_CLI_CONFIG_FILE` (OpenTofu reads
the same block from `~/.tofurc` or `TF_CLI_CONFIG_FILE`):

```hcl
provider_installation {
  dev_overrides {
    "onetsolutions-net/onetsolutions" = "/Users/me/go/bin"
  }
  direct {}
}
```

With an override, skip `terraform init`: `terraform validate`, `plan` and `apply` use the binary
directly (Terraform prints a warning saying so). `examples/complete` validates this way:

```sh
make install
cp -r examples/complete /tmp/complete && cd /tmp/complete
TF_CLI_CONFIG_FILE=/path/to/dev.tfrc terraform validate
```

## Test

| Command | What runs | Cost |
|---|---|---|
| `make test` | unit tests (client, validators, ID parsing, canonical forms), provider tests against the in-process fake API (`internal/acctest/fakeapi`, `resource.UnitTest` with the real `terraform` binary), and the contract test that every route the client can call is in `schema/openapi.json` | free, no credentials |
| `make check` | `gofmt`, `go vet`, `golangci-lint`, `make test` with the race detector, `docs/` up to date, `terraform fmt` of `examples/` — what CI runs | free |
| `make test-tofu` | the tests of `make test`, with OpenTofu (`tofu` on `PATH`) running the configurations instead of Terraform — CI runs it too | free |
| `make vulncheck` | `govulncheck`: known vulnerabilities reachable from the code — CI runs it, with `zizmor` auditing the workflows | free |
| `make testacc` | the acceptance tests (`TestAcc*`) against the **real** API, with `TF_ACC=1` | **billed** |

> [!WARNING]
> `make testacc` **orders real, billed resources** (instances, snapshots). Run it by hand only,
> against a dedicated test project, with the smallest size on the monthly cycle — never in CI.
> Destroying an instance only requests its cancellation at the end of the billing period, so the
> instance the test ordered keeps running, billed, for the rest of its first period.

It needs `ONETSOLUTIONS_API_KEY`, `ONETSOLUTIONS_ORGANIZATION_ID` and `ONETSOLUTIONS_PROJECT_ID`,
plus per test: `ONETSOLUTIONS_ACC_SIZE_ID`, `ONETSOLUTIONS_ACC_LARGER_SIZE_ID` and
`ONETSOLUTIONS_ACC_IMAGE_ID` (instance), `ONETSOLUTIONS_ACC_INSTANCE_ID` (firewall, snapshot,
reverse DNS, on an existing instance), `ONETSOLUTIONS_ACC_IP_ADDRESS` and
`ONETSOLUTIONS_ACC_REVERSE_DNS` (reverse DNS), `ONETSOLUTIONS_ACC_ZONE_NAME` (a domain of the
organization the DNS test may create a zone for). Without `TF_ACC` they skip themselves.

`make sweep` deletes, on the real API, the `tf-acc-*` SSH keys and VPCs and the
`ONETSOLUTIONS_ACC_ZONE_NAME` zone an interrupted run left behind, and requests the cancellation
of the `tf-acc-*` instances: they run, billed, until the end of their period, and a VPC still
attached to one is left until a later sweep.

## Documentation

`make docs` regenerates `docs/` with `tfplugindocs` (run through `go tool`, pinned by `go.mod`)
from the schema descriptions, `examples/` and `templates/`. Commit the result: the registry reads
`docs/` from the release tag, and `make check` fails when it is stale.

- A schema's top-level description is one sentence: it is both the registry subtitle and the
  page's first line. Longer explanations go in the page's template.
- Every page has a template, `templates/{resources,data-sources,actions}/<name>.md.tmpl`, which
  sets its registry `subcategory` (`Account`, `Compute`, `Network`, `DNS & Domains`,
  `Web Hosting`); copy a sibling's for a new page. Billed resources (`instance`,
  `instance_snapshot`, `instance_gpu`, `instance_backup`, `domain`, `web_hosting`) open with the
  billing callout.
- Each resource has `examples/resources/<name>/resource.tf`, `import.sh` and
  `import-by-string-id.tf` (the `import {}` block); each data source
  `examples/data-sources/<name>/data-source.tf`; each action `examples/actions/<name>/action.tf`.
- Action pages need Terraform 1.14 or later on `PATH` when running `make docs`: older versions do
  not report action schemas, and the action pages would be dropped.
- Write for customers: what they can do and what happens, not how the API is implemented.

## Commits and changelog

- One logical change per commit, `make check` green on each.
- Subject in the imperative, capitalised, no trailing period, at most 72 characters
  (`Add onetsolutions_load_balancer`, `Retry a VPC delete while instances detach`); a body that says
  why, wrapped at 72.
- A change a user can notice adds a line under the `Unreleased` heading of
  [`CHANGELOG.md`](CHANGELOG.md), in the section it belongs to (`NOTES`, `FEATURES`,
  `ENHANCEMENTS`, `BUG FIXES`).

## Release

`goreleaser check` validates the configuration; `goreleaser release --snapshot --clean
--skip=sign` builds the archives locally without publishing. A `v*` tag runs
`.github/workflows/release.yml`: HashiCorp's scaffolding (`CGO_ENABLED=0`, the registry's
OS/architecture matrix, zip archives, a GPG-signed `SHA256SUMS`) with
`terraform-registry-manifest.json` (protocol 6.0). The `GPG_PRIVATE_KEY` (ASCII-armoured) and
`PASSPHRASE` secrets live in the `release` environment, which only version tags can use. GoReleaser
uploads to a draft, the archives and checksums get a build provenance attestation
(`gh attestation verify <file> --repo onetsolutions-net/terraform-provider-onetsolutions`), then the
draft is published; releases are immutable. The release notes are the version's section of
`CHANGELOG.md`. The Terraform and OpenTofu registries pick the release up from GitHub.

## Architecture

### Where things live

| Path | What |
|---|---|
| `main.go` | `providerserver.Serve`, `-debug`, `-X main.version` |
| `internal/provider` | provider schema, `Configure`, registry aggregating every area |
| `internal/services/common` | helpers every resource uses (below) |
| `internal/services/base` | `onetsolutions_ssh_key`, `onetsolutions_vpc` (+ data sources): **the reference to copy** |
| `internal/services/account` | `onetsolutions_organization(s)`, `onetsolutions_project(s)` |
| `internal/services/{catalog,compute,network,dns,backup,hosting}` | one area each; `register.go` lists its resources, data sources and actions (`hosting` splits its lists into `register_<group>.go` files) |
| `internal/client/{client,routes,errors,pagination,logging,tasks}.go` | transport, retries, rate limit, errors, `listAll`, task reads |
| `internal/client/{account,base}.go` | organizations, projects, SSH keys, VPCs |
| `internal/client/{catalog,compute,gpu,vpc_ip_allocations,network,dns,domain_order,backup,hosting,hosting_maildb,hosting_web}.go` | that area's typed structs and methods |
| `internal/waiter` | `Wait[T].Run`, `Retry`, `InstanceTask`, `DomainTask` |
| `internal/acctest` | provider factories, provider blocks for the fake, import ID helpers |
| `internal/acctest/fakeapi/{server,store,objects,respond,tasks,seed,account,base}.go` | fake API core, task engine, seeds |
| `internal/acctest/fakeapi/{catalog,compute,gpu,vpc_ip_allocations,network,dns,domain_order,backup,hosting,hosting_maildb,hosting_web}.go` | that area's route handlers (`registerCatalog`, …) and object kinds |
| `internal/contract` | "is this operation in schema/openapi.json or the allow-list?" |
| `examples/resources/<name>/{resource.tf,import.sh}`, `examples/data-sources/<name>/data-source.tf`, `examples/actions/<name>/action.tf` | registry examples |
| `templates/` | `index.md.tmpl` and one template per page; a billed resource's opens with the billing callout |
| `examples/complete` | a full stack (VPC, SSH key, two instances, firewalls, DNS zone, A records) |
| `docs/` | generated by `make docs`, committed |

An area keeps to its own package, its own `internal/client/<area>.go`, its own
`internal/acctest/fakeapi/<area>.go`, its examples and templates; shared helpers live in
`internal/services/common`, `internal/client` and the fake API core.

### Writing a resource

Copy `internal/services/base/vpc_resource.go` (updatable, timeouts, retried delete) or
`ssh_key_resource.go` (immutable, custom type):

- embed `common.ResourceBase`: it provides `Configure`, `Client(&diags)`, `Data()` (provider data,
  `Data().Waiter` is the polling config), `ResolveScope(ctx, org, project)`, `ImportScoped` /
  `ImportScopedChild`, and a `ModifyPlan` that fills `organization_id`/`project_id` on create.
  A resource with its own `ModifyPlan` calls `r.PlanScope(ctx, req, resp)` first.
- attributes: `common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{common.AttrID: common.IDAttribute("thing"), …})`.
- Create: `r.ResolveScope(ctx, plan.OrganizationID, plan.ProjectID)`, call the API, and for an
  asynchronous object call `common.SavePartialState(ctx, &resp.State, id, scope, extra)` **before**
  waiting; on a wait error return the error with that state set.
- waits: `ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, defaults)`,
  then `waiter.Wait[T]{…}.Run(ctx)`, `waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, taskID)`,
  `waiter.DomainTask(…)` or `waiter.Retry(ctx, r.Data().Waiter, "…", op, retryable)` (409 / in use).
- Read: `client.IsNotFound(err)` → `resp.State.RemoveResource(ctx)`.
- errors: `common.AddClientError(&diags, "Could not …", err)`, or `AddClientErrorWithFields` to pin
  the API's field errors to attributes.
- data sources embed `common.DataSourceBase`, use `common.DataSourceScopeAttributes()`,
  `datasourcevalidator.ExactlyOneOf(id, name)` and `common.FindOne` for a lookup by name.

### Writing an action

Actions live in their area's package and are returned by its `Actions()`, which
`internal/provider/registry.go` aggregates in `allActions()`. Copy
`internal/services/backup/power_actions.go`: embed `actionBase` (provider data, the instance target
attributes, task waits), give the schema a `timeouts` block for `invoke`, and report progress with
`resp.SendProgress`. An action stores nothing in the state, so a check that can run before the
apply goes in its `ModifyPlan`. Terraform runs actions from 1.14 and OpenTofu not at all: action
tests use `terraform_data` with an `action_trigger` and skip themselves elsewhere
(`actionVersionChecks` in the backup tests), so `make test-tofu` stays green.

### Writing client methods

In `internal/client/<area>.go`: structs with pointer fields for nullable values, and one
package-level route per operation:

```go
var routeGetThing = newRoute(http.MethodGet, "/v1/organizations/{organization_id}/projects/{project_id}/things/{id}")

func (c *Client) GetThing(ctx context.Context, scope Scope, id string) (*Thing, error) {
	var out Thing
	if err := c.do(ctx, routeGetThing, scope.Params("id", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

`c.do(ctx, route, params, body, out)` sends one request; `listAll[T](ctx, c, route, params, query)`
reads every page (envelope or bare array). Declare routes as package-level variables only:
`TestRoutesAreInTheContract` checks every declared route against `schema/openapi.json`, so an
operation outside the contract fails `make test`. GET/PUT/DELETE are retried on 502/503/504 and
transport errors; a POST/PATCH only on 429, unless declared with `newRetrySafeRoute` (previews).

### Fake API handlers

In `internal/acctest/fakeapi/<area>.go`, fill `register<Area>(s *Server)`:

```go
s.handle(http.MethodGet, thingsTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
	s.writeObject(w, KindThing, scope, "", r.PathValue("id"))
}))
```

- `s.handle(method, template, handler[, anonymous])` registers an OpenAPI path template (Go
  `ServeMux` wildcards, read with `r.PathValue`); a template outside the contract fails the test.
  Authentication is checked for you; `anonymous` routes (the catalog) also answer without a key.
- state: `s.insert / s.lookup / s.list / s.update / s.remove (kind, scope, parentID, …)`; objects
  are `Object` (`map[string]any`, JSON as the API serves it) and are always copies.
- answers: `writeJSON`, `writePage(w, r, items)`, `newestFirst`, `writeError(w, status, detail, code)`,
  `writeFieldErrors(w, fieldErrors{…})`, `writeNotFound`, `decodeBody`, `s.writeObject`, `s.deleteObject`.
- asynchrony: `s.newInstanceTask(scope, instanceID, taskType, params, onComplete)` /
  `s.newDomainTask(…)` queue a task that completes after `WithTaskPolls(n)` reads (default 2) and
  then runs `onComplete`; `s.afterPolls(kind, id, n, change)` changes an object after n reads
  (an instance becoming active). The task routes themselves are served by `tasks.go`.

### Seeding and writing a test

`f := acctest.NewFake(t)` starts a fake with organization "Acme", project "Default" (`f.Scope()`)
and the real catalog (sizes, public images, control panels, hosting plans; `f.First(kind)`).
`Seed*` helpers create any object of any area with the API's shape: `SeedSSHKey`, `SeedVPC`,
`SeedInstance` (with a primary IPv4; `"vpc_id"` override attaches a VPC), `SeedIPAddress`,
`SeedFirewallRule`, `SeedPlatformFirewallRules` (control-plane and One Click Apps rules),
`SeedSnapshot`, `SeedPrivateImage`, `SeedDNSZone`, `SeedDNSRecord`, `SeedDNSSEC`, `SeedDomain`,
`SeedInstanceTask`, `SeedDomainTask`, `SeedWebHosting`, `SeedOrganization`, `SeedProject`,
`SeedGPU`, `SeedGPUAttachment`, `SeedVPCIPAllocation`, `SeedBackup` (on an instance seeded with
`"backup_enabled": true`), `SeedTLD`, `SeedHostingDomain`, `SeedWordPressSite`,
`SeedHostingDatabase`, `SeedHostingDatabaseUser`, `SeedHostingEmail`, `SeedHostingFTPAccount`,
`SeedHostingCronJob`, `SeedHostingSubdomain`, `SeedHostingAddonDomain`, `SeedHostingRedirect`;
`fakeapi.NewPublicKey(comment)` makes a valid key. Outside changes: `f.Delete`, `f.Mutate`;
assertions: `f.Get`, `f.Exists`, `f.All`, `f.CountRequests`; failures: `f.Inject(method, template,
fakeapi.Injection{Status, Body, RetryAfter, Times})`, `f.FailNextTask(taskType, message)`.

```go
func TestThing(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_thing", fakeapi.KindThing),
		Steps: []resource.TestStep{
			{Config: acctest.Config(f, `resource "onetsolutions_thing" "test" { … }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{ResourceName: "onetsolutions_thing.test", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: acctest.ImportIDFunc("onetsolutions_thing.test")},
		},
	})
}
```

Every resource has: create, update in place, replacement, import with `ImportStateVerify` plus an
import-block step asserting a no-op plan, drift (`f.Delete` → the next plan recreates), and an
empty plan after apply. `acctest.ProviderConfigWithoutDefaults` / `ProviderConfigWithScope` cover
the scope rules; `acctest.ChildImportIDFunc` builds child import IDs. Polling runs every few
milliseconds in tests (`acctest.TestWaiter`), so waits cost nothing; use `timeouts` of a few
hundred milliseconds to test a timeout.
