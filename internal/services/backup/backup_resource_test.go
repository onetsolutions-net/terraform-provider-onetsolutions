package backup_test

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
	backupName    = "onetsolutions_instance_backup.test"
	backupsRoute  = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/backups"
	instanceRoute = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{id}"
)

func backupConfig(instanceID, name, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_instance_backup" "test" {
  instance_id = %q
  name        = %q
%s
}
`, instanceID, name, extra)
}

func TestInstanceBackupResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBackupsKept(f),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, backupConfig(instanceID, "before-upgrade", "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("status"), knownvalue.StringExact("completed")),
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("size_gb"), knownvalue.StringExact("3.20")),
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("retention_days"), knownvalue.Int64Exact(7)),
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("backup_type"), knownvalue.StringExact("manual")),
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("expires_at"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("project_id"), knownvalue.StringExact(f.Scope().ProjectID)),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: captureID(&firstID),
			},
			{
				ResourceName:            backupName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ChildImportIDFunc(backupName, "instance_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:      backupName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(backupName, "instance_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(backupName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config: acctest.Config(f, backupConfig(instanceID, "before-upgrade", `
  timeouts {
    create = "4h"
  }`)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(backupName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(*terraform.State) error {
					if n := f.CountRequests(http.MethodPost, backupsRoute); n != 1 {
						return fmt.Errorf("%d backups requested, want 1", n)
					}
					return nil
				},
			},
			{
				Config: acctest.Config(f, backupConfig(instanceID, "after-upgrade", "  retention_days = 3")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(backupName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("retention_days"), knownvalue.Int64Exact(3)),
				},
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						if !f.Exists(fakeapi.KindBackup, firstID) {
							return fmt.Errorf("the replaced backup %s was deleted: it must remain", firstID)
						}
						return nil
					},
					checkIDChanged(&firstID),
				),
			},
		},
	})
}

func TestInstanceBackupResource_newRetentionNeedsNewName(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBackupsKept(f),
		Steps: []resource.TestStep{
			{Config: acctest.Config(f, backupConfig(instanceID, "weekly", ""))},
			{
				Config:      acctest.Config(f, backupConfig(instanceID, "weekly", "  retention_days = 2")),
				ExpectError: regexp.MustCompile(`(?s)A new backup needs a new name.*set a new name as well`),
			},
		},
	})
}

func TestInstanceBackupResource_deletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBackupsKept(f),
		Steps: []resource.TestStep{
			{Config: acctest.Config(f, backupConfig(instanceID, "drift", "")), Check: captureID(&id)},
			{
				PreConfig: func() { f.Delete(fakeapi.KindBackup, id) },
				Config:    acctest.Config(f, backupConfig(instanceID, "drift", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(backupName, plancheck.ResourceActionCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkIDChanged(&id),
			},
		},
	})
}

// An expired backup stays in state with status "deleted": recreating it would reuse its name.
func TestInstanceBackupResource_expiredStaysInState(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()
	config := acctest.Config(f, backupConfig(instanceID, "expiring", "  retention_days = 1"))
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBackupsKept(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&id)},
			{
				PreConfig: func() { f.Mutate(fakeapi.KindBackup, id, func(o fakeapi.Object) { o["status"] = "deleted" }) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("status"), knownvalue.StringExact("deleted")),
				},
			},
		},
	})
}

func TestInstanceBackupResource_requiresBackupOption(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := f.SeedInstance(f.Scope(), nil).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, backupConfig(instanceID, "nightly", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)The instance has no backup option.*paid add-on`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, backupsRoute); n != 0 {
		t.Fatalf("%d backups requested, want none", n)
	}
}

// The option is checked at plan time; an instance created in the same plan is only known at
// apply, where the API's refusal is reported the same way.
func TestInstanceBackupResource_optionRefusedAtApply(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()
	f.Inject(http.MethodGet, instanceRoute, fakeapi.Injection{Status: http.StatusServiceUnavailable, Times: 50})
	f.Mutate(fakeapi.KindInstance, instanceID, func(o fakeapi.Object) { o["backup_enabled"] = false })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, backupConfig(instanceID, "nightly", "")),
				ExpectError: regexp.MustCompile(`(?s)The instance has no backup option`),
			},
		},
	})
}

func TestInstanceBackupResource_waitsWhileInstanceBusy(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	instanceID := seedBackupInstance(f).ID()
	f.SeedInstanceTask(scope, instanceID, "apply_firewall")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBackupsKept(f),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, backupConfig(instanceID, "after-firewall", "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("status"), knownvalue.StringExact("completed")),
				},
				Check: func(*terraform.State) error {
					if n := f.CountRequests(http.MethodPost, backupsRoute); n < 2 {
						return fmt.Errorf("%d backup requests, want a retry after the busy refusal", n)
					}
					return nil
				},
			},
		},
	})
}

// A failed backup stays in state, tainted, with status "failed"; its name stays taken, so the
// replacement needs a new one.
func TestInstanceBackupResource_failedTask(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()
	f.FailNextTask("create_backup", "the backup storage is full")
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBackupsKept(f),
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, backupConfig(instanceID, "nightly", "")),
				ExpectError: regexp.MustCompile(`the\s+backup\s+storage\s+is\s+full`),
			},
			{
				Config:      acctest.Config(f, backupConfig(instanceID, "nightly", "")),
				ExpectError: regexp.MustCompile(`(?s)This backup name is taken.*"nightly".*failed`),
			},
			{
				Config: acctest.Config(f, backupConfig(instanceID, "nightly-retry", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(backupName, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(backupName, tfjsonpath.New("status"), knownvalue.StringExact("completed")),
				},
				Check: captureID(&id),
			},
		},
	})
}

func TestInstanceBackupResource_timeout(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t, fakeapi.WithTaskPolls(100000))
	instanceID := seedBackupInstance(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkBackupsKept(f),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, backupConfig(instanceID, "slow", `
  timeouts {
    create = "2s"
  }`)),
				ExpectError: regexp.MustCompile(`timed\s+out`),
			},
		},
	})
}

func TestInstanceBackupResource_validation(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, backupConfig(instanceID, "weekly", "  retention_days = 8")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)retention_days.*between 1 and 7`),
			},
			{
				Config:      acctest.Config(f, backupConfig(instanceID, " weekly", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)whitespace`),
			},
			{
				Config:      acctest.Config(f, backupConfig("not-a-uuid", "weekly", "")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid UUID`),
			},
		},
	})
}
