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

const dnssecName = "onetsolutions_dns_zone_dnssec.test"

func dnssecConfig(zoneID string, enabled bool) string {
	return fmt.Sprintf(`
resource "onetsolutions_dns_zone_dnssec" "test" {
  zone_id = %q
  enabled = %t
}
`, zoneID, enabled)
}

func dnssecStatus(f *fakeapi.Server, zoneID, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		state, ok := f.Get(fakeapi.KindDNSSEC, zoneID)
		if !ok || state["status"] != want {
			return fmt.Errorf("DNSSEC status = %v, want %s", state["status"], want)
		}
		return nil
	}
}

func TestDNSSECResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), fakeapi.Object{"zone_name": "signed.example"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			state, _ := f.Get(fakeapi.KindDNSSEC, zone.ID())
			if state["status"] != "pending_disabled" && state["status"] != "disabled" {
				return fmt.Errorf("destroy left DNSSEC %v", state["status"])
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, dnssecConfig(zone.ID(), true)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(dnssecName, tfjsonpath.New("id"), knownvalue.StringExact(zone.ID())),
					statecheck.ExpectKnownValue(dnssecName, tfjsonpath.New("enabled"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(dnssecName, tfjsonpath.New("ds_record"), knownvalue.StringRegexp(regexp.MustCompile(`IN DS 2371 13 2`))),
					statecheck.ExpectKnownValue(dnssecName, tfjsonpath.New("key_tag"), knownvalue.Int64Exact(2371)),
				},
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				// The registrar published the DS record: the status settles without a diff.
				Config:           acctest.Config(f, dnssecConfig(zone.ID(), true)),
				Check:            resource.ComposeTestCheckFunc(dnssecStatus(f, zone.ID(), "active"), resource.TestCheckResourceAttr(dnssecName, "status", "active")),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				ResourceName:      dnssecName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ImportIDFunc(dnssecName),
				ImportStateVerify: true,
			},
			{
				ResourceName:    dnssecName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   zone.ID(),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(dnssecName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// Disabled outside Terraform: drift, planned back on.
				PreConfig: func() {
					f.Mutate(fakeapi.KindDNSSEC, zone.ID(), func(o fakeapi.Object) { o["status"] = "disabled" })
				},
				Config: acctest.Config(f, dnssecConfig(zone.ID(), true)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(dnssecName, plancheck.ResourceActionUpdate)},
				},
				Check: dnssecStatus(f, zone.ID(), "pending"),
			},
			{
				Config: acctest.Config(f, dnssecConfig(zone.ID(), false)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(dnssecName, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr(dnssecName, "enabled", "false"),
			},
			{
				Config: acctest.Config(f, dnssecConfig(zone.ID(), true)),
				Check:  dnssecStatus(f, zone.ID(), "pending"),
			},
		},
	})
}

func TestDNSSECResource_zoneDeletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), nil)
	config := acctest.Config(f, dnssecConfig(zone.ID(), true))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { f.Delete(fakeapi.KindDNSZone, zone.ID()) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

const settingsName = "onetsolutions_domain_settings.test"

func TestDomainSettingsResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	domain := f.SeedDomain(scope, fakeapi.Object{"name": "acme.fr"})
	config := func(body string) string {
		return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_domain_settings" "test" {
  domain_id = %q
%s
}`, domain.ID(), body))
	}
	stored := func(key string, want any) resource.TestCheckFunc {
		return func(*terraform.State) error {
			current, _ := f.Get(fakeapi.KindDomain, domain.ID())
			if fmt.Sprint(current[key]) != fmt.Sprint(want) {
				return fmt.Errorf("domain %s = %v, want %v", key, current[key], want)
			}
			return nil
		}
	}
	managed := `  nameservers   = ["ada.ns.onetsolutions.net", "bob.ns.onetsolutions.net"]
  auto_renew    = false
  locked        = false
  whois_privacy = true`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			current, ok := f.Get(fakeapi.KindDomain, domain.ID())
			if !ok || current["whois_privacy"] != false || current["auto_renew"] != true {
				return fmt.Errorf("destroy must leave the domain as it is: %v", current)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Nothing configured: the resource adopts the domain and reports its settings.
				Config: config(""),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(settingsName, tfjsonpath.New("name"), knownvalue.StringExact("acme.fr")),
					statecheck.ExpectKnownValue(settingsName, tfjsonpath.New("locked"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(settingsName, tfjsonpath.New("nameservers"), knownvalue.ListSizeExact(2)),
				},
				Check: func(*terraform.State) error {
					if n := len(f.All(fakeapi.KindDomainTask)); n != 0 {
						return fmt.Errorf("%d domain tasks, want 0", n)
					}
					return nil
				},
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				Config: config(managed),
				Check: resource.ComposeTestCheckFunc(
					stored("nameservers", []any{"ada.ns.onetsolutions.net", "bob.ns.onetsolutions.net"}),
					stored("auto_renew", false),
					stored("is_locked", false),
					stored("whois_privacy", true),
				),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				ResourceName:      settingsName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ImportIDFunc(settingsName),
				ImportStateVerify: true,
			},
			{
				ResourceName:    settingsName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   domain.ID(),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(settingsName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// Changed outside Terraform: drift, put back.
				PreConfig: func() {
					f.Mutate(fakeapi.KindDomain, domain.ID(), func(o fakeapi.Object) { o["auto_renew"] = true })
				},
				Config: config(managed),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(settingsName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: stored("auto_renew", false),
			},
			{
				// Back to the original values before destroy leaves the domain alone.
				Config: config(`  auto_renew    = true
  whois_privacy = false`),
				Check: resource.ComposeTestCheckFunc(stored("auto_renew", true), stored("whois_privacy", false), stored("is_locked", false)),
			},
		},
	})
}

func TestDomainSettingsResource_errors(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	domain := f.SeedDomain(f.Scope(), nil)
	config := func(body string) string {
		return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_domain_settings" "test" {
  domain_id = %q
%s
}`, domain.ID(), body))
	}
	f.FailNextTask("lock_domain", "registry refused")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config(`  nameservers = ["ns1.example.net"]`), ExpectError: regexp.MustCompile(`list must contain at least 2`)},
			{Config: config(`  nameservers = ["NS1.example.net", "ns2.example.net"]`), ExpectError: regexp.MustCompile(`must be lowercase`)},
			{Config: config(`  nameservers = ["ns1.example.net.", "ns2.example.net"]`), ExpectError: regexp.MustCompile(`must not end with a dot`)},
			{
				PreConfig:   func() { f.Mutate(fakeapi.KindDomain, domain.ID(), func(o fakeapi.Object) { o["is_locked"] = false }) },
				Config:      config(`  locked = true`),
				ExpectError: regexp.MustCompile(`(?s)Changing the lock of the domain failed.*ended failed`),
			},
			{Config: config(`  locked = true`), Check: resource.TestCheckResourceAttr(settingsName, "locked", "true")},
		},
	})
	if n := f.CountRequests(http.MethodPut, nameserversURL); n != 0 {
		t.Fatalf("%d name server updates, want 0", n)
	}
}

func TestDomainDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	zone := f.SeedDNSZone(scope, fakeapi.Object{"zone_name": "shop.fr"})
	domain := f.SeedDomain(scope, fakeapi.Object{"name": "shop.fr", "dns_zone": zone})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_domain" "by_name" {
  name = "shop.fr"
}
data "onetsolutions_domain" "by_id" {
  id = %q
}
`, domain.ID())),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_domain.by_name", tfjsonpath.New("id"), knownvalue.StringExact(domain.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_domain.by_name", tfjsonpath.New("dns_zone_id"), knownvalue.StringExact(zone.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_domain.by_name", tfjsonpath.New("expiration_date"), knownvalue.StringExact("2027-01-01")),
					statecheck.ExpectKnownValue("data.onetsolutions_domain.by_id", tfjsonpath.New("locked"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("data.onetsolutions_domain.by_id", tfjsonpath.New("recurring_amount"), knownvalue.StringExact("9.99")),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_domain" "x" { name = "nope.fr" }`),
				ExpectError: regexp.MustCompile(`No domain matches name "nope.fr"`),
			},
			{
				Config: acctest.Config(f, `data "onetsolutions_domain" "x" { name = "shop.fr" }`),
			},
		},
	})
}
