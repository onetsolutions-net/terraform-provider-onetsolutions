package compute_test

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

// The API checks start/stop against a power status of its own, which can already hold the
// target while the synced state still reads the other one: its "already running" refusal means
// the desired state is reached, and the provider waits for the synced state to catch up.
func TestInstanceResource_alreadyInStateRefusalCountsAsReached(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, ""), Check: captureID(&id)},
			{
				PreConfig:        func() { f.DesyncPowerState(id, "stopped", 4) },
				Config:           instanceConfig(f, c, c.base, ""),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(instanceName, "state", "running"),
					checkVMStatus(f, &id, "running"),
					expectRequests(f, http.MethodPost, startRoute, 1),
					expectTasks(f, fakeapi.TaskStartInstance, 0),
				),
			},
		},
	})
}

// A new instance with state = "stopped" whose power state is not synced yet once installed is
// stopped once the API reports it running: the provider never writes a state it did not observe.
func TestInstanceResource_createStoppedWaitsForAKnownPowerState(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithPowerSyncDelay(3))
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:           instanceConfig(f, c, c.base, `  state = "stopped"`),
			ConfigPlanChecks: emptyPlanAfterApply(),
			Check: resource.ComposeTestCheckFunc(
				captureID(&id),
				resource.TestCheckResourceAttr(instanceName, "state", "stopped"),
				expectTasks(f, fakeapi.TaskStopInstance, 1),
				checkVMStatus(f, &id, "stopped"),
			),
		}},
	})
}

// A power state that never becomes known fails the create once the timeout is reached, with the
// instance kept in the state, instead of recording a state nobody observed.
func TestInstanceResource_createFailsWhenThePowerStateStaysUnknown(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithPowerSyncDelay(1_000_000))
	c := sizesOf(t, f)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: instanceConfig(f, c, c.base, `  state = "stopped"
  timeouts {
    create = "2s"
  }`),
			ExpectError: regexp.MustCompile(`(?s)power state could not be set.*has not reported whether.*terraform untaint`),
		}},
	})
	if n := f.CountRequests(http.MethodPost, stopRoute); n != 0 {
		t.Fatalf("stop sent %d times while the power state was unknown", n)
	}
}

// A resize needs a known stopped instance: with the power state unknown at apply (the refreshed
// state still says stopped, so the plan-time check passes), nothing is ordered on a VM that may
// be running.
func TestInstanceResource_resizeRefusedWhileThePowerStateIsUnknown(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, `  state = "stopped"`), Check: captureID(&id)},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) { o["state_sync"] = fakeapi.Object{"vm_status": "paused"} })
				},
				Config: instanceConfig(f, c, c.larger, `  state = "stopped"
  timeouts {
    update = "1s"
  }`),
				ExpectError: regexp.MustCompile(`(?s)Could not resize the instance.*resize was not ordered.*has not reported whether`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, resizeRoute); n != 0 {
		t.Fatalf("resize ordered %d times while the power state was unknown", n)
	}
}
