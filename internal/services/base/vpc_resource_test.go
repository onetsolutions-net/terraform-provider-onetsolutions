package base_test

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
	vpcName  = "onetsolutions_vpc.test"
	vpcRoute = "/v1/organizations/{organization_id}/projects/{project_id}/compute/vpcs/{id}"
)

func vpcConfig(name, cidr, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_vpc" "test" {
  name       = %q
  cidr_block = %q
%s
}
`, name, cidr, extra)
}

func TestVPCResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_vpc", fakeapi.KindVPC),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, vpcConfig("tf-acc-vpc", "10.20.0.0/16", "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(vpcName, tfjsonpath.New("cidr_block"), knownvalue.StringExact("10.20.0.0/16")),
				},
				Check: captureID(vpcName, &firstID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:            vpcName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ImportIDFunc(vpcName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:      vpcName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ImportIDFunc(vpcName),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(vpcName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config: acctest.Config(f, vpcConfig("tf-acc-vpc-renamed", "10.20.0.0/16", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(vpcName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(vpcName, &firstID),
					resource.TestCheckResourceAttr(vpcName, "name", "tf-acc-vpc-renamed"),
				),
			},
			{
				Config: acctest.Config(f, vpcConfig("tf-acc-vpc-renamed", "10.30.0.0/16", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(vpcName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}

func TestVPCResource_invalidCIDRAtPlan(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, vpcConfig("bad", "10.0.0.1/16", "")),
				ExpectError: regexp.MustCompile(`host bits set`),
			},
			{
				Config:      acctest.Config(f, vpcConfig("bad", "fd00::/64", "")),
				ExpectError: regexp.MustCompile(`not an IPv4 network`),
			},
		},
	})
}

func TestVPCResource_deleteRetriesWhileInUse(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	f.Inject(http.MethodDelete, vpcRoute, fakeapi.Injection{
		Status: http.StatusBadRequest,
		Body:   fakeapi.Object{"detail": "Cannot delete VPC that is in use by instances", "code": "vpc_in_use"},
		Times:  2,
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if n := f.CountRequests(http.MethodDelete, vpcRoute); n != 3 {
				return fmt.Errorf("DELETE sent %d times, want 3 (two refusals, then success)", n)
			}
			return acctest.CheckDestroyed(f, "onetsolutions_vpc", fakeapi.KindVPC)(s)
		},
		Steps: []resource.TestStep{
			{Config: acctest.Config(f, vpcConfig("tf-acc-in-use", "10.40.0.0/16", ""))},
		},
	})
}

func TestVPCResource_deleteTimesOutWhileInstancesAttached(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	timeouts := `  timeouts {
    delete = "2s"
  }`
	config := acctest.Config(f, vpcConfig("tf-acc-attached", "10.50.0.0/16", timeouts))
	var instanceID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(s *terraform.State) error {
					vpcID := s.RootModule().Resources[vpcName].Primary.ID
					instanceID = f.SeedInstance(f.Scope(), fakeapi.Object{"vpc_id": vpcID}).ID()
					return nil
				},
			},
			{
				Config:      config,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`Cannot delete VPC that is in use by instances`),
			},
			{
				PreConfig: func() { f.Delete(fakeapi.KindInstance, instanceID) },
				Config:    config,
			},
		},
	})
}

// The scoping rules of plan section 4, shown on the VPC: the provider defaults apply on create
// only; an explicit, different project_id replaces the object.
func TestVPCResource_scopeDefaultsApplyOnCreateOnly(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	other := f.SeedProject(scope.OrganizationID, "Other").ID()
	otherScope := fakeapi.Scope{OrganizationID: scope.OrganizationID, ProjectID: other}
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, vpcConfig("tf-acc-scope", "10.60.0.0/16", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(vpcName, tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
					},
				},
				Check: captureID(vpcName, &firstID),
			},
			{
				Config: acctest.ProviderConfigWithScope(f, otherScope) + vpcConfig("tf-acc-scope", "10.60.0.0/16", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(vpcName, &firstID),
					resource.TestCheckResourceAttr(vpcName, "project_id", scope.ProjectID),
				),
			},
			{
				Config: acctest.ProviderConfigWithScope(f, otherScope) +
					vpcConfig("tf-acc-scope", "10.60.0.0/16", fmt.Sprintf("  project_id = %q", scope.ProjectID)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: acctest.Config(f, vpcConfig("tf-acc-scope", "10.60.0.0/16", fmt.Sprintf("  project_id = %q", other))),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(vpcName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.TestCheckResourceAttr(vpcName, "project_id", other),
			},
		},
	})
}

func TestVPCResource_lazyScopeResolution(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.ProviderConfigWithoutDefaults(f) + vpcConfig("tf-acc-lazy", "10.70.0.0/16", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue(vpcName, tfjsonpath.New("organization_id"), knownvalue.StringExact(scope.OrganizationID)),
						plancheck.ExpectKnownValue(vpcName, tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
					},
				},
			},
			{
				// A second project ends the "single project" resolution without touching the VPC.
				PreConfig: func() { f.SeedProject(scope.OrganizationID, "Second") },
				Config:    acctest.ProviderConfigWithoutDefaults(f) + vpcConfig("tf-acc-lazy", "10.70.0.0/16", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: acctest.ProviderConfigWithoutDefaults(f) + vpcConfig("tf-acc-lazy", "10.70.0.0/16", "") + `
resource "onetsolutions_vpc" "second" {
  name       = "tf-acc-lazy-2"
  cidr_block = "10.71.0.0/16"
}
`,
				ExpectError: regexp.MustCompile(`(?s)project_id is required.*has 2 projects`),
			},
		},
	})
}
