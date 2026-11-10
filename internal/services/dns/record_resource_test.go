package dns_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
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

// recordsConfig writes every spelling the DNS service canonicalises: the apex as "@" and as the
// zone name, a relative and a fully qualified name with its trailing dot, TXT values quoted and
// not, a target with its trailing dot, and a proxied record whose TTL the API forces.
func recordsConfig(zoneID, webAddress string, webTTL int) string {
	return fmt.Sprintf(`
locals {
  zone_id = %q
}
resource "onetsolutions_dns_record" "apex" {
  zone_id = local.zone_id
  type    = "A"
  name    = "@"
  content = "203.0.113.10"
}
resource "onetsolutions_dns_record" "www" {
  zone_id = local.zone_id
  type    = "A"
  name    = "www"
  content = %q
  ttl     = %d
}
resource "onetsolutions_dns_record" "api_fqdn" {
  zone_id = local.zone_id
  type    = "AAAA"
  name    = "API.example.com."
  content = "2001:db8:0:0::1"
}
resource "onetsolutions_dns_record" "apex_by_zone_name" {
  zone_id = local.zone_id
  type    = "TXT"
  name    = "example.com"
  content = "v=spf1 include:_spf.example.net -all"
}
resource "onetsolutions_dns_record" "quoted_txt" {
  zone_id = local.zone_id
  type    = "TXT"
  name    = "_dmarc"
  content = "\"v=DMARC1; p=none\""
}
resource "onetsolutions_dns_record" "mx" {
  zone_id  = local.zone_id
  type     = "MX"
  name     = "@"
  content  = "Mail.Example.net."
  priority = 10
}
resource "onetsolutions_dns_record" "proxied" {
  zone_id = local.zone_id
  type    = "CNAME"
  name    = "shop"
  content = "shops.example.net"
  proxied = true
}
`, zoneID, webAddress, webTTL)
}

func TestRecordResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), fakeapi.Object{"zone_name": "example.com"})
	var wwwID string

	stored := func(resourceName string, want map[string]any) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			record, ok := f.Get(fakeapi.KindDNSRecord, s.RootModule().Resources[resourceName].Primary.ID)
			if !ok {
				return fmt.Errorf("%s is not in the fake", resourceName)
			}
			for key, value := range want {
				if fmt.Sprint(record[key]) != fmt.Sprint(value) {
					return fmt.Errorf("%s: %s = %v, want %v", resourceName, key, record[key], value)
				}
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_dns_record", fakeapi.KindDNSRecord),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, recordsConfig(zone.ID(), "203.0.113.20", 300)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("onetsolutions_dns_record.apex", tfjsonpath.New("name"), knownvalue.StringExact("@")),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.apex", tfjsonpath.New("ttl"), knownvalue.Int64Exact(3600)),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.api_fqdn", tfjsonpath.New("name"), knownvalue.StringExact("API.example.com.")),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.api_fqdn", tfjsonpath.New("content"), knownvalue.StringExact("2001:db8:0:0::1")),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.apex_by_zone_name", tfjsonpath.New("name"), knownvalue.StringExact("example.com")),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.quoted_txt", tfjsonpath.New("content"), knownvalue.StringExact(`"v=DMARC1; p=none"`)),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.mx", tfjsonpath.New("content"), knownvalue.StringExact("Mail.Example.net.")),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.mx", tfjsonpath.New("priority"), knownvalue.Int64Exact(10)),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.proxied", tfjsonpath.New("ttl"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.proxied", tfjsonpath.New("proxied"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("onetsolutions_dns_record.www", tfjsonpath.New("ttl"), knownvalue.Int64Exact(300)),
				},
				Check: resource.ComposeTestCheckFunc(
					captureID("onetsolutions_dns_record.www", &wwwID),
					stored("onetsolutions_dns_record.api_fqdn", map[string]any{"name": "api.example.com", "content": "2001:db8::1"}),
					stored("onetsolutions_dns_record.apex_by_zone_name", map[string]any{
						"name": "example.com", "content": `"v=spf1 include:_spf.example.net -all"`,
					}),
					stored("onetsolutions_dns_record.mx", map[string]any{"content": "mail.example.net"}),
				),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				ResourceName:      "onetsolutions_dns_record.www",
				ImportState:       true,
				ImportStateIdFunc: acctest.ChildImportIDFunc("onetsolutions_dns_record.www", "zone_id"),
				ImportStateVerify: true,
			},
			{
				// An import writes the canonical spelling ("@", unquoted TXT, the target as the API
				// returns it): a configuration in that spelling plans nothing.
				ResourceName:      "onetsolutions_dns_record.apex",
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: childImportShortID("onetsolutions_dns_record.apex"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("onetsolutions_dns_record.apex", plancheck.ResourceActionNoop),
					},
				},
			},
			{
				// Content and TTL change in place, keeping the record's ID.
				Config: acctest.Config(f, recordsConfig(zone.ID(), "203.0.113.21", 600)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("onetsolutions_dns_record.www", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPtr("onetsolutions_dns_record.www", "id", &wwwID),
					stored("onetsolutions_dns_record.www", map[string]any{"content": "203.0.113.21", "ttl": 600}),
				),
			},
			{
				// Deleted outside Terraform: created again.
				PreConfig: func() { f.Delete(fakeapi.KindDNSRecord, wwwID) },
				Config:    acctest.Config(f, recordsConfig(zone.ID(), "203.0.113.21", 600)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("onetsolutions_dns_record.www", plancheck.ResourceActionCreate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Changed outside Terraform: the next plan puts it back.
				PreConfig: func() {
					for _, record := range f.All(fakeapi.KindDNSRecord) {
						if record["name"] == "www.example.com" {
							f.Mutate(fakeapi.KindDNSRecord, record.ID(), func(o fakeapi.Object) { o["content"] = "198.51.100.1" })
						}
					}
				},
				Config:             acctest.Config(f, recordsConfig(zone.ID(), "203.0.113.21", 600)),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func childImportShortID(resourceName string) func(*terraform.State) (string, error) {
	return func(s *terraform.State) (string, error) {
		attrs := s.RootModule().Resources[resourceName].Primary.Attributes
		return attrs["zone_id"] + "/" + attrs["id"], nil
	}
}

func TestRecordResource_proxiedToggle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), fakeapi.Object{"zone_name": "example.com"})
	config := func(proxied bool) string {
		return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_dns_record" "test" {
  zone_id = %q
  type    = "A"
  name    = "edge"
  content = "203.0.113.30"
  proxied = %t
}`, zone.ID(), proxied))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(false),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("onetsolutions_dns_record.test", tfjsonpath.New("ttl"), knownvalue.Int64Exact(3600)),
				},
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				Config: config(true),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("onetsolutions_dns_record.test", tfjsonpath.New("ttl"), knownvalue.Int64Exact(1)),
				},
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				Config:           config(false),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
		},
	})
}

func TestRecordResource_invalidAtPlan(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), fakeapi.Object{"zone_name": "example.com"})
	record := func(body string) string {
		return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_dns_record" "test" {
  zone_id = %q
  name    = "x"
%s
}`, zone.ID(), body))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      record("  type = \"MX\"\n  content = \"mail.example.net\""),
				ExpectError: regexp.MustCompile(`A MX record needs a priority`),
			},
			{
				Config:      record("  type = \"TXT\"\n  content = \"hello\"\n  proxied = true"),
				ExpectError: regexp.MustCompile(`Only A, AAAA and CNAME records can be proxied`),
			},
			{
				Config:      record("  type = \"A\"\n  content = \"203.0.113.1\"\n  proxied = true\n  ttl = 300"),
				ExpectError: regexp.MustCompile(`A proxied record has an automatic TTL`),
			},
			{
				Config:      record("  type = \"A\"\n  content = \"2001:db8::1\""),
				ExpectError: regexp.MustCompile(`is not an IPv4 address`),
			},
			{
				Config:      record("  type = \"A\"\n  content = \"203.0.113.1\"\n  ttl = 30"),
				ExpectError: regexp.MustCompile(`Invalid TTL`),
			},
			{
				Config:      record("  type = \"SOA\"\n  content = \"x\""),
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, recordsRoute); n != 0 {
		t.Fatalf("%d record creations, want 0", n)
	}
}

func TestRecordResource_zoneDeletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), fakeapi.Object{"zone_name": "gone.example"})
	config := acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_dns_record" "test" {
  zone_id = %q
  type    = "TXT"
  name    = "@"
  content = "hello"
}`, zone.ID()))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ConfigPlanChecks: emptyPlanAfterApply()},
			{
				PreConfig:          func() { f.Delete(fakeapi.KindDNSZone, zone.ID()) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// A TXT value longer than 255 bytes comes back split into several quoted strings ("a" "b"): it
// is the same record, so the state keeps the configured spelling and the next plan is empty.
func TestDNSRecordResource_longTXTSplitIntoStrings(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	zone := f.SeedDNSZone(f.Scope(), fakeapi.Object{"zone_name": "example.com"})
	key := "v=DKIM1; k=rsa; p=" + strings.Repeat("MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA", 8)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_dns_record" "dkim" {
  zone_id = %q
  type    = "TXT"
  name    = "mail._domainkey"
  content = %q
}`, zone.ID(), key)),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("onetsolutions_dns_record.dkim", "content", key),
				func(*terraform.State) error {
					for _, record := range f.All(fakeapi.KindDNSRecord) {
						if strings.Contains(record.String("content"), `" "`) {
							return nil
						}
					}
					return fmt.Errorf("the fake did not split the long TXT value into strings")
				},
			),
		}},
	})
}
