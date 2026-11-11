package hosting_test

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
	subdomainName        = "onetsolutions_hosting_subdomain.test"
	subdomainDeleteRoute = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/subdomains/{subdomain}/delete"
)

func subdomainConfig(hostingID, label, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_subdomain" "test" {
  hosting_id = %q
  subdomain  = %q
  %s
}
`, hostingID, label, extra)
}

func TestSubdomainResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := seedHosting(f)
	hostingID, domain := hosting.ID(), hosting.String("domain")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_hosting_subdomain", fakeapi.KindHostingSubdomain),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, subdomainConfig(hostingID, "blog", "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(subdomainName, tfjsonpath.New("id"), knownvalue.StringExact("blog."+domain)),
					statecheck.ExpectKnownValue(subdomainName, tfjsonpath.New("domain"), knownvalue.StringExact(domain)),
					statecheck.ExpectKnownValue(subdomainName, tfjsonpath.New("document_root"), knownvalue.StringExact("/home/seeded/public_html/blog")),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkChildren(f, fakeapi.KindHostingSubdomain, "blog."+domain),
			},
			{
				ResourceName:      subdomainName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ChildImportIDFunc(subdomainName, "hosting_id"),
				ImportStateVerify: true,
			},
			{
				// The label alone designates the subdomain too.
				ResourceName:      subdomainName,
				ImportState:       true,
				ImportStateId:     hostingID + "/blog",
				ImportStateVerify: true,
			},
			{
				ResourceName:      subdomainName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(subdomainName, "hosting_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(subdomainName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config: acctest.Config(f, subdomainConfig(hostingID, "shop", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(subdomainName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkChildren(f, fakeapi.KindHostingSubdomain, "shop."+domain),
			},
			{
				Config: acctest.Config(f, subdomainConfig(hostingID, "shop", `document_root = "sites/shop"`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(subdomainName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(*terraform.State) error {
					subdomain, ok := f.Get(fakeapi.KindHostingSubdomain, "shop."+domain)
					if !ok || subdomain.String("document_root") != "sites/shop" {
						return fmt.Errorf("subdomain in the API: %v", subdomain)
					}
					return nil
				},
			},
			{
				// Imported, a subdomain takes the configured document root as it is, without a
				// replacement.
				ResourceName:       subdomainName,
				ImportState:        true,
				ImportStateKind:    resource.ImportBlockWithID,
				ImportStateIdFunc:  acctest.ChildImportIDFunc(subdomainName, "hosting_id"),
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(subdomainName, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}

func TestSubdomainResource_deletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := seedHosting(f)
	config := acctest.Config(f, subdomainConfig(hosting.ID(), "blog", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() { f.Delete(fakeapi.KindHostingSubdomain, "blog."+hosting.String("domain")) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(subdomainName, plancheck.ResourceActionCreate)},
				},
				Check: checkChildren(f, fakeapi.KindHostingSubdomain, "blog."+hosting.String("domain")),
			},
			{
				// Gone by the time of the destroy: nothing is sent.
				PreConfig: func() { f.Delete(fakeapi.KindHostingSubdomain, "blog."+hosting.String("domain")) },
				Config:    acctest.Config(f),
				Check: func(*terraform.State) error {
					if n := f.CountRequests(http.MethodDelete, subdomainDeleteRoute); n != 0 {
						return fmt.Errorf("delete sent %d times for a subdomain already gone", n)
					}
					return nil
				},
			},
		},
	})
}

func TestSubdomainResource_hostingGone(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	config := acctest.Config(f, subdomainConfig(hostingID, "blog", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { f.Delete(fakeapi.KindWebHosting, hostingID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestSubdomainResource_errors(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	f.SeedHostingSubdomain(f.Scope(), hostingID, "taken")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, subdomainConfig(hostingID, "Blog", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be lowercase labels`),
			},
			{
				Config:      acctest.Config(f, subdomainConfig(hostingID, "taken", "")),
				ExpectError: regexp.MustCompile(`Subdomain already exists`),
			},
			{
				Config:      acctest.Config(f, subdomainConfig(hostingID, "blog", "")),
				PreConfig:   func() { terminateHosting(f, hostingID) },
				ExpectError: regexp.MustCompile(`Could not list the subdomains`),
			},
		},
	})
}
