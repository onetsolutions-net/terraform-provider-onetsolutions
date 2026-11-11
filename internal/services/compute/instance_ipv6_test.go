package compute_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

const ipv6Route = instanceRoute + "/ipv6"

func TestInstanceResource_ipv6OnCreate(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{
				Config:           instanceConfig(f, c, c.base, "  ipv6_enabled = true"),
				ConfigPlanChecks: emptyPlanAfterApply(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("ipv6_enabled"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("primary_ipv6"), knownvalue.StringRegexp(regexp.MustCompile(`^2001:db8:`))),
				},
				Check: expectTasks(f, fakeapi.TaskEnableIPv6, 1),
			},
			{
				ResourceName:            instanceName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ImportIDFunc(instanceName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: createOnlyAttributes,
			},
		},
	})
}

func TestInstanceResource_ipv6InPlace(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{
				Config:           instanceConfig(f, c, c.base, ""),
				ConfigPlanChecks: emptyPlanAfterApply(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("ipv6_enabled"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("primary_ipv6"), knownvalue.Null()),
				},
				Check: resource.ComposeTestCheckFunc(captureID(&id), expectRequests(f, http.MethodPost, ipv6Route, 0)),
			},
			{
				Config: instanceConfig(f, c, c.base, "  ipv6_enabled = true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(instanceName, plancheck.ResourceActionUpdate),
						plancheck.ExpectUnknownValue(instanceName, tfjsonpath.New("primary_ipv6")),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("primary_ipv6"), knownvalue.NotNull()),
				},
				Check: resource.ComposeTestCheckFunc(checkIDUnchanged(&id), expectTasks(f, fakeapi.TaskEnableIPv6, 1)),
			},
			{
				Config:      instanceConfig(f, c, c.base, "  ipv6_enabled = false"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`IPv6 cannot be disabled`),
			},
			{
				// Unset, it follows the instance.
				Config:           instanceConfig(f, c, c.base, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
	})
}

// IPv6 enabled in the console is read, never silently reverted: a configuration that says false
// is refused at plan time, one that leaves it unset plans nothing.
func TestInstanceResource_ipv6EnabledOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	enable := func() {
		f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) { o["primary_ipv6"] = "2001:db8:42::1" })
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, "  ipv6_enabled = false"), Check: captureID(&id)},
			{
				PreConfig:   enable,
				Config:      instanceConfig(f, c, c.base, "  ipv6_enabled = false"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)IPv6 cannot be disabled.*2001:db8:42::1`),
			},
			{
				Config:           instanceConfig(f, c, c.base, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("ipv6_enabled"), knownvalue.Bool(true)),
				},
			},
		},
	})
}

// An enable refused while another task holds the instance is retried.
func TestInstanceResource_ipv6RetriedWhileBusy(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, ""), Check: captureID(&id)},
			{
				PreConfig:        func() { f.SeedInstanceTask(f.Scope(), id, "reboot") },
				Config:           instanceConfig(f, c, c.base, "  ipv6_enabled = true"),
				ConfigPlanChecks: emptyPlanAfterApply(),
				Check: resource.ComposeTestCheckFunc(
					expectTasks(f, fakeapi.TaskEnableIPv6, 1),
					checkFake(func() error {
						if n := f.CountRequests(http.MethodPost, ipv6Route); n < 2 {
							return fmt.Errorf("enable sent %d times, want a retry after the refusal", n)
						}
						return nil
					}),
				),
			},
		},
	})
}
