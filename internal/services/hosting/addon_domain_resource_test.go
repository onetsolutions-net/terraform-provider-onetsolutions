package hosting_test

import (
	"fmt"
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

const addonDomainName = "onetsolutions_hosting_addon_domain.test"

func addonDomainConfig(hostingID, domain, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_addon_domain" "test" {
  hosting_id = %q
  domain     = %q
  %s
}
`, hostingID, domain, extra)
}

func checkAddonDomain(f *fakeapi.Server, domain, field, want string) func(*terraform.State) error {
	return func(*terraform.State) error {
		addon, ok := f.Get(fakeapi.KindHostingAddonDomain, domain)
		if !ok {
			return fmt.Errorf("addon domain %s not in the API", domain)
		}
		if got := addon.String(field); got != want {
			return fmt.Errorf("addon domain %s in the API is %q, want %q", field, got, want)
		}
		return nil
	}
}

func TestAddonDomainResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_hosting_addon_domain", fakeapi.KindHostingAddonDomain),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, addonDomainConfig(hostingID, "example.org", "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addonDomainName, tfjsonpath.New("id"), knownvalue.StringExact("example.org")),
					statecheck.ExpectKnownValue(addonDomainName, tfjsonpath.New("subdomain"), knownvalue.StringExact("example-org")),
					statecheck.ExpectKnownValue(addonDomainName, tfjsonpath.New("document_root"), knownvalue.StringExact("/home/seeded/public_html/example.org")),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      addonDomainName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ChildImportIDFunc(addonDomainName, "hosting_id"),
				ImportStateVerify: true,
			},
			{
				ResourceName:      addonDomainName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(addonDomainName, "hosting_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addonDomainName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// A relative document root is kept as configured: the API reports it as an
				// absolute path.
				Config: acctest.Config(f, addonDomainConfig(hostingID, "example.org", `document_root = "sites/example.org"`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addonDomainName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addonDomainName, tfjsonpath.New("document_root"), knownvalue.StringExact("sites/example.org")),
				},
				Check: checkAddonDomain(f, "example.org", "document_root", "/home/seeded/sites/example.org"),
			},
			{
				Config: acctest.Config(f, addonDomainConfig(hostingID, "example.org", `document_root = "sites/example.org"`+"\n  subdomain = \"org\"")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addonDomainName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkAddonDomain(f, "example.org", "subdomain", "org"),
			},
			{
				// A document root changed outside Terraform shows as a replacement.
				PreConfig: func() {
					f.Mutate(fakeapi.KindHostingAddonDomain, "example.org", func(o fakeapi.Object) { o["document_root"] = "/home/seeded/elsewhere" })
				},
				Config: acctest.Config(f, addonDomainConfig(hostingID, "example.org", `document_root = "sites/example.org"`+"\n  subdomain = \"org\"")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addonDomainName, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: checkAddonDomain(f, "example.org", "document_root", "/home/seeded/sites/example.org"),
			},
			{
				Config: acctest.Config(f, addonDomainConfig(hostingID, "example.net", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(addonDomainName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkChildren(f, fakeapi.KindHostingAddonDomain, "example.net"),
			},
		},
	})
}

func TestAddonDomainResource_deletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	config := acctest.Config(f, addonDomainConfig(hostingID, "example.org", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { f.Delete(fakeapi.KindHostingAddonDomain, "example.org") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addonDomainName, plancheck.ResourceActionCreate)},
				},
				Check: checkChildren(f, fakeapi.KindHostingAddonDomain, "example.org"),
			},
		},
	})
}

func TestAddonDomainResource_errors(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	f.SeedHostingAddonDomain(f.Scope(), hostingID, "taken.org")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, addonDomainConfig(hostingID, "Example.org.", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be a lowercase domain name`),
			},
			{
				Config:      acctest.Config(f, addonDomainConfig(hostingID, "example.org", `subdomain = "a.b"`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`without dots`),
			},
			{
				Config:      acctest.Config(f, addonDomainConfig(hostingID, "taken.org", "")),
				ExpectError: regexp.MustCompile(`Addon domain already exists`),
			},
		},
	})
}
