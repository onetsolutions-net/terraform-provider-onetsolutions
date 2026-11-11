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
	redirectName        = "onetsolutions_hosting_redirect.test"
	redirectCreateRoute = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/redirects/create"
)

func redirectConfig(hostingID, domain, source, destination, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_redirect" "test" {
  hosting_id  = %q
  domain      = %q
  source      = %q
  destination = %q
  %s
}
`, hostingID, domain, source, destination, extra)
}

func redirectsIn(f *fakeapi.Server) []string {
	var out []string
	for _, r := range f.All(fakeapi.KindHostingRedirect) {
		out = append(out, r.String("domain")+r.String("source")+" "+r.String("type")+" "+r.String("destination"))
	}
	return out
}

func checkRedirects(f *fakeapi.Server, want ...string) func(*terraform.State) error {
	return func(*terraform.State) error {
		if got := redirectsIn(f); fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("redirects in the API: got %v, want %v", got, want)
		}
		return nil
	}
}

func TestRedirectResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := seedHosting(f)
	hostingID, domain := hosting.ID(), hosting.String("domain")
	old := domain + "/old 301 https://example.com/new"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Destroying only forgets a redirect: Terraform cannot remove one.
		CheckDestroy: checkRedirects(f, old, domain+"/blog 302 https://blog.example.com/"),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, redirectConfig(hostingID, domain, "/old", "https://example.com/new", "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(redirectName, tfjsonpath.New("id"), knownvalue.StringExact(domain+"/old")),
					statecheck.ExpectKnownValue(redirectName, tfjsonpath.New("type"), knownvalue.StringExact("301")),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkRedirects(f, old),
			},
			{
				ResourceName:      redirectName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ChildImportIDFunc(redirectName, "hosting_id"),
				ImportStateVerify: true,
			},
			{
				ResourceName:      redirectName,
				ImportState:       true,
				ImportStateIdFunc: shortImportID(redirectName),
				ImportStateVerify: true,
			},
			{
				ResourceName:      redirectName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(redirectName, "hosting_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(redirectName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// Another destination or type for the same source needs the old redirect removed
				// first: refused at plan time, the state kept.
				Config:      acctest.Config(f, redirectConfig(hostingID, domain, "/old", "https://example.com/other", "")),
				ExpectError: regexp.MustCompile(`A redirect cannot be changed in place`),
			},
			{
				Config:      acctest.Config(f, redirectConfig(hostingID, domain, "/old", "https://example.com/new", `type = "302"`)),
				ExpectError: regexp.MustCompile(`A redirect cannot be changed in place`),
			},
			{
				Config: acctest.Config(f, redirectConfig(hostingID, domain, "/old", "https://example.com/new", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkRedirects(f, old),
			},
			{
				// Destroying only forgets it.
				Config: acctest.Config(f, ""),
				Check:  checkRedirects(f, old),
			},
			{
				// The same redirect again: the one in place is taken over, not added twice.
				Config: acctest.Config(f, redirectConfig(hostingID, domain, "/old", "https://example.com/new", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(redirectName, plancheck.ResourceActionCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkRedirects(f, old),
					func(*terraform.State) error {
						if n := f.CountRequests(http.MethodPost, redirectCreateRoute); n != 1 {
							return fmt.Errorf("create sent %d times, want 1", n)
						}
						return nil
					},
				),
			},
			{
				Config: acctest.Config(f, redirectConfig(hostingID, domain, "/blog", "https://blog.example.com/", `type = "302"`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(redirectName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(redirectName, tfjsonpath.New("type"), knownvalue.StringExact("302")),
				},
				Check: checkRedirects(f, old, domain+"/blog 302 https://blog.example.com/"),
			},
		},
	})
}

func TestRedirectResource_deletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := seedHosting(f)
	domain := hosting.String("domain")
	config := acctest.Config(f, redirectConfig(hosting.ID(), domain, "/old", "https://example.com/new", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					for _, r := range f.All(fakeapi.KindHostingRedirect) {
						f.Delete(fakeapi.KindHostingRedirect, r.ID())
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(redirectName, plancheck.ResourceActionCreate)},
				},
				Check: checkRedirects(f, domain+"/old 301 https://example.com/new"),
			},
		},
	})
}

func TestRedirectResource_errors(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := seedHosting(f)
	hostingID, domain := hosting.ID(), hosting.String("domain")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, redirectConfig(hostingID, domain, "old", "https://example.com/", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be a path starting with /`),
			},
			{
				Config:      acctest.Config(f, redirectConfig(hostingID, domain, "/old", "ftp://example.com/", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`is not an http or https URL`),
			},
			{
				Config:      acctest.Config(f, redirectConfig(hostingID, domain, "/old", "https://example.com/", `type = "307"`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}
