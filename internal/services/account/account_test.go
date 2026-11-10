package account_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
)

func TestOrganizationDataSources(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	other := f.SeedOrganization("Globex")
	f.SeedOrganization("Twin")
	f.SeedOrganization("Twin")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_organization" "by_name" {
  name = "Acme"
}
data "onetsolutions_organization" "by_id" {
  id = %q
}
data "onetsolutions_organizations" "all" {}
`, other["id"])),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_organization.by_name", tfjsonpath.New("id"), knownvalue.StringExact(scope.OrganizationID)),
					statecheck.ExpectKnownValue("data.onetsolutions_organization.by_name", tfjsonpath.New("project_count"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue("data.onetsolutions_organization.by_name", tfjsonpath.New("country"), knownvalue.StringExact("FR")),
					statecheck.ExpectKnownValue("data.onetsolutions_organization.by_id", tfjsonpath.New("name"), knownvalue.StringExact("Globex")),
					statecheck.ExpectKnownValue("data.onetsolutions_organizations.all", tfjsonpath.New("organizations"), knownvalue.ListSizeExact(4)),
					statecheck.ExpectKnownValue("data.onetsolutions_organizations.all",
						tfjsonpath.New("organizations").AtSliceIndex(0).AtMapKey("id"), knownvalue.StringExact(scope.OrganizationID)),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_organization" "x" { name = "Twin" }`),
				ExpectError: regexp.MustCompile(`2 objects of type organization match name "Twin"`),
			},
		},
	})
}

func TestProjectDataSources(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	staging := f.SeedProject(scope.OrganizationID, "Staging")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_project" "by_name" {
  name = "Staging"
}
data "onetsolutions_project" "by_id" {
  organization_id = %q
  id              = %q
}
data "onetsolutions_projects" "all" {}
`, scope.OrganizationID, scope.ProjectID)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_project.by_name", tfjsonpath.New("id"), knownvalue.StringExact(staging.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_project.by_name", tfjsonpath.New("organization_id"), knownvalue.StringExact(scope.OrganizationID)),
					statecheck.ExpectKnownValue("data.onetsolutions_project.by_id", tfjsonpath.New("name"), knownvalue.StringExact("Default")),
					statecheck.ExpectKnownValue("data.onetsolutions_project.by_id", tfjsonpath.New("domain_quota"), knownvalue.Int64Exact(10)),
					statecheck.ExpectKnownValue("data.onetsolutions_projects.all", tfjsonpath.New("projects"), knownvalue.ListSizeExact(2)),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_project" "x" { name = "Production" }`),
				ExpectError: regexp.MustCompile(`No project matches name "Production"`),
			},
		},
	})
}

func TestProjectsDataSource_organizationResolution(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// One organization: found without any setting.
				Config: acctest.ProviderConfigWithoutDefaults(f) + `data "onetsolutions_projects" "all" {}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_projects.all", tfjsonpath.New("organization_id"), knownvalue.StringExact(scope.OrganizationID)),
				},
			},
			{
				PreConfig:   func() { f.SeedOrganization("Second") },
				Config:      acctest.ProviderConfigWithoutDefaults(f) + `data "onetsolutions_projects" "all" {}`,
				ExpectError: regexp.MustCompile(`(?s)organization_id is required.*belongs to 2 organizations`),
			},
		},
	})
}
