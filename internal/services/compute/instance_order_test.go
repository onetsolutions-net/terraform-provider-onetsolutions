package compute_test

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

const instanceTasksRoute = instancesRoute + "/{instance_id}/tasks"

// An order the account balance does not cover creates no instance until it is paid: the create
// stops at once with the invoice to pay, the local order id and the import to run, instead of
// waiting out its timeout for an instance that cannot appear, and leaves nothing in the state.
// Once the order is paid, importing by that order id adopts its instance, and the configuration
// then plans nothing.
func TestInstanceResource_unpaidOrderThenImportByOrder(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithUnpaidOrders())
	c := sizesOf(t, f)
	config := instanceConfig(f, c, c.base, "")
	var localOrderID, instanceID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: instanceConfig(f, c, c.base, `  timeouts {
    create = "2s"
  }`),
				ExpectError: regexp.MustCompile(`(?s)The instance order awaits payment.*local\s+order\s+id\s+[0-9a-f-]{36}.*` +
					`invoice\s+900001.*billing/invoices/900001.*terraform\s+import\s+<address>\s+order:[0-9a-f-]{36}.*second\s+instance`),
			},
			{
				// Importing before the payment finds nothing to import.
				PreConfig: func() {
					unpaid := f.UnpaidOrders()
					if len(unpaid) != 1 {
						t.Fatalf("%d unpaid orders, want 1", len(unpaid))
					}
					localOrderID = unpaid[0]
				},
				Config:            config,
				ResourceName:      instanceName,
				ImportState:       true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return "order:" + localOrderID, nil },
				ExpectError:       regexp.MustCompile(`(?s)The order has no instance yet.*Pay its invoice`),
			},
			{
				PreConfig: func() {
					instanceID = f.PayOrder(localOrderID).ID()
					for range fakeapi.DefaultTaskPolls {
						f.Get(fakeapi.KindInstance, instanceID)
					}
				},
				Config:             config,
				ResourceName:       instanceName,
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return "order:" + localOrderID, nil },
				ImportStatePersist: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].ID != instanceID || states[0].Attributes["order_id"] != localOrderID {
						return fmt.Errorf("imported %v, want instance %s of order %s", states, instanceID, localOrderID)
					}
					return nil
				},
			},
			{
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
		},
		CheckDestroy: acctest.CheckInstancesCancelled(f),
	})
	if n := f.CountRequests(http.MethodPost, instancesRoute); n != 1 {
		t.Fatalf("ordered %d times, want 1", n)
	}
}

// An import by order whose instance is not the order's (an API that ignores the filter) adopts
// nothing, and a malformed order id is refused before any request.
func TestInstanceResource_importByOrderRefusesWithoutOrderLink(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithoutOrderLink())
	c := sizesOf(t, f)
	f.SeedInstance(f.Scope(), fakeapi.Object{"name": "tf-acc-web"})
	config := instanceConfig(f, c, c.base, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:        config,
				ResourceName:  instanceName,
				ImportState:   true,
				ImportStateId: "order:44444444-4444-4444-8444-444444444444",
				ExpectError:   regexp.MustCompile(`(?s)Could not find the instance of the order.*did\s+not\s+report\s+which\s+order`),
			},
			{
				Config:        config,
				ResourceName:  instanceName,
				ImportState:   true,
				ImportStateId: "order:not-a-uuid",
				ExpectError:   regexp.MustCompile(`(?s)Invalid import ID.*lowercase UUID`),
			},
		},
	})
}

// Waiting for the tasks a change queues reads only the most recent tasks: the API lists them
// newest first, so a long history is never paged through.
func TestInstanceResource_renameReadsOnlyTheRecentTasks(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	named := func(name string) string {
		return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_instance" "test" {
  name     = %q
  size_id  = %q
  image_id = %q
}
`, name, c.base, c.image))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: named("tf-acc-web"), Check: captureID(&id)},
			{
				PreConfig: func() {
					for range 250 {
						f.SeedInstanceTask(f.Scope(), id, "sync_vps_state")
					}
				},
				Config: named("tf-acc-api"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(instanceName, "name", "tf-acc-api"),
					expectTasks(f, fakeapi.TaskChangeHostname, 1),
					expectRequests(f, http.MethodGet, instanceTasksRoute, 2),
				),
			},
		},
	})
}
