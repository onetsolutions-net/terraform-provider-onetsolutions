package dns_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

const (
	zoneName       = "onetsolutions_dns_zone.test"
	zonesRoute     = "/v1/organizations/{organization_id}/projects/{project_id}/dns-zones"
	zoneRoute      = zonesRoute + "/{id}"
	recordsRoute   = zoneRoute + "/records"
	recordRoute    = recordsRoute + "/{record_id}"
	dnssecRoute    = zoneRoute + "/dnssec"
	domainRoute    = "/v1/organizations/{organization_id}/projects/{project_id}/domains/{id}"
	nameserversURL = domainRoute + "/nameservers"
)

func zoneConfig(name string) string {
	return fmt.Sprintf(`
resource "onetsolutions_dns_zone" "test" {
  zone_name = %q
}
`, name)
}

func emptyPlanAfterApply() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

func captureID(name string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func TestZoneResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_dns_zone", fakeapi.KindDNSZone),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, zoneConfig("example.com")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(zoneName, tfjsonpath.New("zone_name"), knownvalue.StringExact("example.com")),
					statecheck.ExpectKnownValue(zoneName, tfjsonpath.New("mode"), knownvalue.StringExact("onetsolutions")),
					statecheck.ExpectKnownValue(zoneName, tfjsonpath.New("nameservers"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact("ada.ns.onetsolutions.net"), knownvalue.StringExact("bob.ns.onetsolutions.net"),
					})),
					statecheck.ExpectKnownValue(zoneName, tfjsonpath.New("is_verified"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(zoneName, tfjsonpath.New("domain_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue(zoneName, tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
				},
				Check:            captureID(zoneName, &firstID),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				ResourceName:      zoneName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ImportIDFunc(zoneName),
				ImportStateVerify: true,
			},
			{
				ResourceName:    zoneName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources[zoneName].Primary.ID, nil
				},
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(zoneName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// Deleted outside Terraform: planned for creation again.
				PreConfig: func() { f.Delete(fakeapi.KindDNSZone, firstID) },
				Config:    acctest.Config(f, zoneConfig("example.com")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(zoneName, plancheck.ResourceActionCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: acctest.Config(f, zoneConfig("example.org")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(zoneName, plancheck.ResourceActionDestroyBeforeCreate)},
				},
			},
		},
	})
}

func TestZoneResource_invalidAtPlan(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: acctest.Config(f, zoneConfig("Example.com")), ExpectError: regexp.MustCompile(`must be lowercase`)},
			{Config: acctest.Config(f, zoneConfig("example.com.")), ExpectError: regexp.MustCompile(`must not end with a dot`)},
			{Config: acctest.Config(f, zoneConfig("localhost")), ExpectError: regexp.MustCompile(`not a domain name`)},
			{
				Config: acctest.Config(f, `
resource "onetsolutions_dns_zone" "test" {
  zone_name = "example.com"
  mode      = "client"
}`),
				ExpectError: regexp.MustCompile(`A client zone cannot be created`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, zonesRoute); n != 0 {
		t.Fatalf("%d zone creations, want 0: invalid zones must fail at plan", n)
	}
}

func TestZoneResource_createTimeoutKeepsTracking(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t, fakeapi.WithTaskPolls(1_000_000))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, `
resource "onetsolutions_dns_zone" "test" {
  zone_name = "slow.example"
  timeouts {
    create = "1s"
  }
}`),
				ExpectError: regexp.MustCompile(`(?s)timed out waiting for DNS zone slow.example.*stays in`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					rs := s.RootModule().Resources[zoneName]
					if rs == nil || !rs.Primary.Tainted || !f.Exists(fakeapi.KindDNSZone, rs.Primary.ID) {
						return fmt.Errorf("the zone must stay in the state, tainted: %+v", rs)
					}
					return nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if n := len(f.All(fakeapi.KindDNSZone)); n != 0 {
				return fmt.Errorf("%d zones left, want 0", n)
			}
			return nil
		},
	})
}

func TestZoneDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), fakeapi.Object{"zone_name": "seeded.example"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_dns_zone" "by_name" {
  zone_name = "seeded.example"
}
data "onetsolutions_dns_zone" "by_id" {
  id = %q
}
`, zone.ID())),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_dns_zone.by_name", tfjsonpath.New("id"), knownvalue.StringExact(zone.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_dns_zone.by_name", tfjsonpath.New("nameservers"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("data.onetsolutions_dns_zone.by_id", tfjsonpath.New("zone_name"), knownvalue.StringExact("seeded.example")),
					statecheck.ExpectKnownValue("data.onetsolutions_dns_zone.by_id", tfjsonpath.New("is_verified"), knownvalue.Bool(true)),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_dns_zone" "x" { zone_name = "missing.example" }`),
				ExpectError: regexp.MustCompile(`No DNS zone matches zone_name "missing.example"`),
			},
			{
				Config: acctest.Config(f, `data "onetsolutions_dns_zone" "x" { zone_name = "seeded.example" }`),
			},
		},
	})
}
