package hosting_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

const (
	phpVersionName  = "onetsolutions_hosting_php_version.test"
	setPHPRoute     = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/php-version"
	phpVersionsName = "data.onetsolutions_hosting_php_versions.test"
)

func phpVersionConfig(f *fakeapi.Server, hostingID, body string) string {
	return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_hosting_php_version" "test" {
  hosting_id = %q
%s
}
`, hostingID, body))
}

func phpActions(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(phpVersionName, action)},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func TestPHPVersionResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-php.fr"})
	f.SeedHostingDomain(hosting.ID(), "blog.tf-acc-php.fr")
	id := hosting.ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			for _, domain := range []string{"tf-acc-php.fr", "blog.tf-acc-php.fr"} {
				if v := f.HostingPHPVersion(id, domain); v != "inherit" && v != fakeapi.DefaultPHPVersion {
					return fmt.Errorf("%s still runs %s after the destroy", domain, v)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Without a domain, the primary domain.
				Config:           phpVersionConfig(f, id, `  php_version = "ea-php83"`),
				ConfigPlanChecks: phpActions(plancheck.ResourceActionCreate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(phpVersionName, "id", "tf-acc-php.fr"),
					resource.TestCheckResourceAttr(phpVersionName, "domain", "tf-acc-php.fr"),
					func(*terraform.State) error {
						if v := f.HostingPHPVersion(id, "tf-acc-php.fr"); v != "ea-php83" {
							return fmt.Errorf("primary domain runs %s", v)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      phpVersionName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ChildImportIDFunc(phpVersionName, "hosting_id"),
				ImportStateVerify: true,
			},
			{
				ResourceName:      phpVersionName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(phpVersionName, "hosting_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(phpVersionName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config:           phpVersionConfig(f, id, `  php_version = "ea-php82"`),
				ConfigPlanChecks: phpActions(plancheck.ResourceActionUpdate),
			},
			{
				// Setting the primary domain explicitly targets the same domain.
				Config:           phpVersionConfig(f, id, "  domain      = \"tf-acc-php.fr\"\n  php_version = \"ea-php82\""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// Another domain is another resource: the primary domain gets its default back.
				Config:           phpVersionConfig(f, id, "  domain      = \"blog.tf-acc-php.fr\"\n  php_version = \"ea-php82\""),
				ConfigPlanChecks: phpActions(plancheck.ResourceActionDestroyBeforeCreate),
				Check: func(*terraform.State) error {
					if v := f.HostingPHPVersion(id, "tf-acc-php.fr"); v != "inherit" {
						return fmt.Errorf("primary domain runs %s after the replacement, want inherit", v)
					}
					return nil
				},
			},
			{
				// Changed outside Terraform: the next plan sets it back.
				PreConfig:        func() { f.SetHostingPHPVersion(id, "blog.tf-acc-php.fr", "ea-php74") },
				Config:           phpVersionConfig(f, id, "  domain      = \"blog.tf-acc-php.fr\"\n  php_version = \"ea-php82\""),
				ConfigPlanChecks: phpActions(plancheck.ResourceActionUpdate),
			},
		},
	})
}

// A domain that inherits is listed with the version it runs: inherit stays in the state, on
// create and on update, and the next plan is empty.
func TestPHPVersionResource_inherit(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	id := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-inherit.fr"}).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           phpVersionConfig(f, id, `  php_version = "inherit"`),
				ConfigPlanChecks: phpActions(plancheck.ResourceActionCreate),
				Check:            resource.TestCheckResourceAttr(phpVersionName, "php_version", "inherit"),
			},
			{
				Config:           phpVersionConfig(f, id, `  php_version = "ea-php83"`),
				ConfigPlanChecks: phpActions(plancheck.ResourceActionUpdate),
				Check:            resource.TestCheckResourceAttr(phpVersionName, "php_version", "ea-php83"),
			},
			{
				Config:           phpVersionConfig(f, id, `  php_version = "inherit"`),
				ConfigPlanChecks: phpActions(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(phpVersionName, "php_version", "inherit"),
					func(*terraform.State) error {
						if v := f.HostingPHPVersion(id, "tf-acc-inherit.fr"); v != "inherit" {
							return fmt.Errorf("domain runs %s, want inherit", v)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestPHPVersionResource_checksAndDrift(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := f.SeedWebHosting(f.Scope(), nil)
	pending := f.SeedWebHosting(f.Scope(), fakeapi.Object{"status": "pending"})
	id := hosting.ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      phpVersionConfig(f, id, `  php_version = "ea-php99"`),
				ExpectError: regexp.MustCompile(`PHP version not offered`),
			},
			{
				Config:      phpVersionConfig(f, id, "  domain      = \"unknown.example.fr\"\n  php_version = \"ea-php82\""),
				ExpectError: regexp.MustCompile(`Check that unknown.example.fr is a domain`),
			},
			{
				Config:      phpVersionConfig(f, pending.ID(), `  php_version = "ea-php82"`),
				ExpectError: regexp.MustCompile(`only while its web\s+hosting is active`),
			},
			{Config: phpVersionConfig(f, id, `  php_version = "inherit"`)},
			{
				// The web hosting ended: the resource is forgotten and created again on a new one.
				PreConfig:          func() { f.Mutate(fakeapi.KindWebHosting, id, func(o fakeapi.Object) { o["status"] = "terminated" }) },
				Config:             phpVersionConfig(f, id, `  php_version = "inherit"`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				PreConfig: func() { f.Mutate(fakeapi.KindWebHosting, id, func(o fakeapi.Object) { o["status"] = "active" }) },
				Config:    phpVersionConfig(f, id, `  php_version = "inherit"`),
			},
			{
				// Gone with its web hosting: forgotten by the next refresh, destroyed without a request.
				PreConfig:        func() { f.Delete(fakeapi.KindWebHosting, id) },
				Config:           acctest.Config(f, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
	})
	// The unknown domain, the pending web hosting and the creation; nothing for the forgotten one.
	if n := f.CountRequests(http.MethodPost, setPHPRoute); n != 3 {
		t.Fatalf("%d PHP version requests, want 3", n)
	}
}

func TestPHPVersionsDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-list.fr"})
	f.SeedHostingDomain(hosting.ID(), "shop.tf-acc-list.fr")
	f.SetHostingPHPVersion(hosting.ID(), "shop.tf-acc-list.fr", "ea-php83")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_hosting_php_versions" "test" {
  hosting_id = %q
}
`, hosting.ID())),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(phpVersionsName, "versions.#", fmt.Sprint(len(fakeapi.InstalledPHPVersions))),
					resource.TestCheckResourceAttr(phpVersionsName, "versions.0.version", fakeapi.InstalledPHPVersions[0]),
					resource.TestCheckResourceAttr(phpVersionsName, "domains.#", "2"),
					resource.TestCheckResourceAttr(phpVersionsName, "domains.0.domain", "tf-acc-list.fr"),
					resource.TestCheckResourceAttr(phpVersionsName, "domains.0.php_version", fakeapi.DefaultPHPVersion),
					resource.TestCheckResourceAttr(phpVersionsName, "domains.1.php_version", "ea-php83"),
					resource.TestCheckResourceAttr(phpVersionsName, "project_id", f.Scope().ProjectID),
				),
			},
			{
				Config: acctest.Config(f, `
data "onetsolutions_hosting_php_versions" "test" {
  hosting_id = "00000000-0000-4000-8000-000000000000"
}
`),
				ExpectError: regexp.MustCompile(`Could not list the PHP versions`),
			},
		},
	})
}
