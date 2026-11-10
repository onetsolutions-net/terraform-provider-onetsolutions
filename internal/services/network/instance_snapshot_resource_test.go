package network_test

import (
	"fmt"
	"net/http"
	"os"
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
	snapshotName   = "onetsolutions_instance_snapshot.test"
	snapshotsRoute = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/snapshots"
)

func snapshotConfig(instanceID, name, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_instance_snapshot" "test" {
  instance_id = %q
  name        = %q
%s
}
`, instanceID, name, extra)
}

func TestInstanceSnapshotResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_snapshot", fakeapi.KindSnapshot),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, snapshotConfig(instanceID, "tf-acc-before-upgrade", `  description = "Before the upgrade"`)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(snapshotName, tfjsonpath.New("status"), knownvalue.StringExact("present")),
					statecheck.ExpectKnownValue(snapshotName, tfjsonpath.New("size_gb"), knownvalue.StringExact("2.35")),
					statecheck.ExpectKnownValue(snapshotName, tfjsonpath.New("instance_id"), knownvalue.StringExact(instanceID)),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: captureID(snapshotName, &firstID),
			},
			{
				ResourceName:            snapshotName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ChildImportIDFunc(snapshotName, "instance_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:      snapshotName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(snapshotName, "instance_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(snapshotName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config: acctest.Config(f, snapshotConfig(instanceID, "tf-acc-before-upgrade", `  description = "Kept for a week"`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(snapshotName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkIDChanged(snapshotName, &firstID),
					func(*terraform.State) error {
						if f.Exists(fakeapi.KindSnapshot, firstID) {
							return fmt.Errorf("the replaced snapshot %s still exists", firstID)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestInstanceSnapshotResource_deletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	config := acctest.Config(f, snapshotConfig(instanceID, "tf-acc-drift", ""))
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_snapshot", fakeapi.KindSnapshot),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(snapshotName, &id)},
			{
				PreConfig: func() { f.Delete(fakeapi.KindSnapshot, id) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(snapshotName, plancheck.ResourceActionCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkIDChanged(snapshotName, &id),
			},
		},
	})
}

// A failed snapshot task leaves the snapshot in state, tainted, so the next apply replaces it
// instead of ordering a second one alongside.
func TestInstanceSnapshotResource_failedTaskIsTracked(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	f.FailNextTask("create_snapshot", "not enough space on the storage")
	config := acctest.Config(f, snapshotConfig(instanceID, "tf-acc-tainted", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_snapshot", fakeapi.KindSnapshot),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`not\s+enough\s+space\s+on\s+the\s+storage`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(snapshotName, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(snapshotName, tfjsonpath.New("status"), knownvalue.StringExact("present")),
				},
			},
		},
	})
}

func TestInstanceSnapshotResource_retriesWhileBusy(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	f.SeedInstanceTask(f.Scope(), instanceID, "reboot")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_snapshot", fakeapi.KindSnapshot),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, snapshotConfig(instanceID, "tf-acc-busy", "")),
				Check: func(*terraform.State) error {
					if n := f.CountRequests(http.MethodPost, snapshotsRoute); n < 2 {
						return fmt.Errorf("POST snapshot sent %d times, want a retry after the 409", n)
					}
					return nil
				},
			},
		},
	})
}

func TestInstanceSnapshotResource_invalidNameAtPlan(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, snapshotConfig(instanceID, "Before-Upgrade", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must\s+be\s+lowercase\s+letters`),
			},
			{
				Config:      acctest.Config(f, snapshotConfig(instanceID, "2026-11-10", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not\s+starting\s+with\s+a\s+digit`),
			},
			{
				Config:      acctest.Config(f, snapshotConfig(instanceID, "ab", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`length\s+must\s+be\s+between\s+3\s+and\s+255`),
			},
			{
				// The API trims the description: a trailing newline would be an inconsistent result.
				Config:      acctest.Config(f, snapshotConfig(instanceID, "before-upgrade", `  description = "before the upgrade\n"`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Surrounding whitespace`),
			},
		},
	})
}

func TestAccInstanceSnapshot_basic(t *testing.T) {
	t.Parallel()
	instanceID := os.Getenv(envAccInstanceID)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t); accPreCheck(t)() },
		ProtoV6ProviderFactories: acctest.RealProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: snapshotConfig(instanceID, "tf-acc-snapshot", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:            snapshotName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ChildImportIDFunc(snapshotName, "instance_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}
