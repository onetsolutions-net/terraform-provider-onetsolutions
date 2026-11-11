package backup_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

const instancesRoute = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances"

// triggered declares an action and a terraform_data that invokes it once created and on every
// change of its input.
func triggered(actionType, actionConfig, input string) string {
	return fmt.Sprintf(`
action %[1]q "test" {
  config {
%[2]s
  }
}

resource "terraform_data" "trigger" {
  input = %[3]q

  lifecycle {
    action_trigger {
      events  = [after_create, after_update]
      actions = [action.%[1]s.test]
    }
  }
}
`, actionType, actionConfig, input)
}

// checkTasks verifies the fake ran exactly want tasks of taskType, all completed.
func checkTasks(f *fakeapi.Server, taskType string, want int) func(*terraform.State) error {
	return func(*terraform.State) error {
		var got int
		for _, task := range f.All(fakeapi.KindInstanceTask) {
			if task["task_type"] != taskType {
				continue
			}
			got++
			if task["status"] != "completed" {
				return fmt.Errorf("task %s (%s) is %v, want completed", task.ID(), taskType, task["status"])
			}
		}
		if got != want {
			return fmt.Errorf("%d %s tasks, want %d", got, taskType, want)
		}
		return nil
	}
}

func TestInstanceRebootAction(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := f.SeedInstance(f.Scope(), nil).ID()
	config := fmt.Sprintf("    instance_id = %q", instanceID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_reboot", config, "first")),
				Check:  checkTasks(f, fakeapi.TaskRebootInstance, 1),
			},
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_reboot", config, "second")),
				Check:  checkTasks(f, fakeapi.TaskRebootInstance, 2),
			},
		},
	})
}

func TestInstanceResetAction(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := f.SeedInstance(f.Scope(), nil).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_reset", fmt.Sprintf(`    instance_id     = %q
    organization_id = %q
    project_id      = %q`, instanceID, f.Scope().OrganizationID, f.Scope().ProjectID), "first")),
				Check: checkTasks(f, fakeapi.TaskResetInstance, 1),
			},
		},
	})
}

func TestInstanceRebootAction_failedTask(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := f.SeedInstance(f.Scope(), nil).ID()
	f.FailNextTask(fakeapi.TaskRebootInstance, "the instance did not answer")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, triggered("onetsolutions_instance_reboot", fmt.Sprintf("    instance_id = %q", instanceID), "first")),
				ExpectError: regexp.MustCompile(`(?s)The reboot did not complete.*the\s+instance\s+did\s+not\s+answer`),
			},
		},
	})
}

func TestInstanceRebootAction_timeout(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t, fakeapi.WithTaskPolls(100000))
	instanceID := f.SeedInstance(f.Scope(), nil).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, `
action "onetsolutions_instance_reboot" "test" {
  config {
    instance_id = "`+instanceID+`"

    timeouts {
      invoke = "300ms"
    }
  }
}

resource "terraform_data" "trigger" {
  input = "first"

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.onetsolutions_instance_reboot.test]
    }
  }
}
`),
				ExpectError: regexp.MustCompile(`timed\s+out`),
			},
		},
	})
}

func TestInstanceRebootAction_missingInstance(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, triggered("onetsolutions_instance_reboot", `    instance_id = "9a0b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"`, "first")),
				ExpectError: regexp.MustCompile(`Could not start the reboot`),
			},
		},
	})
}

func TestInstanceReinstallAction(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	instance := f.SeedInstance(scope, nil)
	instanceID := instance.ID()
	f.SeedSnapshot(scope, instanceID, nil)
	key := f.SeedSSHKey(scope, nil)
	var other fakeapi.Object
	for _, image := range f.All(fakeapi.KindPublicImage) {
		if image.ID() != instance.Map("image").ID() {
			other = image
			break
		}
	}
	if other == nil {
		t.Fatal("the catalog needs two public images")
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_reinstall", fmt.Sprintf(`    instance_id          = %q
    image_id             = %q
    ssh_key_ids          = [%q]
    allow_password_reset = false

    one_click_app {
      slug       = "wordpress"
      parameters = { site_title = "Blog" }
    }`, instanceID, other.ID(), key.ID()), "first")),
				Check: resource.ComposeTestCheckFunc(
					checkTasks(f, fakeapi.TaskReinstallInstance, 1),
					func(*terraform.State) error {
						current, _ := f.Get(fakeapi.KindInstance, instanceID)
						if got := current.Map("image").ID(); got != other.ID() {
							return fmt.Errorf("instance image = %s, want %s", got, other.ID())
						}
						if len(f.All(fakeapi.KindSnapshot)) != 0 {
							return fmt.Errorf("the reinstall kept the instance's snapshots")
						}
						for _, task := range f.All(fakeapi.KindInstanceTask) {
							if task["task_type"] != fakeapi.TaskReinstallInstance {
								continue
							}
							params := task.Map("parameters")
							keys := params.List("ssh_key_ids")
							apps := params.List("one_click_apps")
							if params["allow_password_reset"] != false || params["install_management_key"] != true ||
								len(keys) != 1 || keys[0] != key.ID() || len(apps) != 1 {
								return fmt.Errorf("reinstall parameters = %v", params)
							}
							app, _ := apps[0].(map[string]any)
							parameters, _ := app["parameters"].(map[string]any)
							if app["slug"] != "wordpress" || parameters["site_title"] != "Blog" {
								return fmt.Errorf("one click app = %v", app)
							}
						}
						return nil
					},
				),
			},
		},
	})
}

func TestInstanceReinstallAction_unknownImage(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := f.SeedInstance(f.Scope(), nil).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_reinstall", fmt.Sprintf(`    instance_id = %q
    image_id    = "9a0b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"`, instanceID), "first")),
				ExpectError: regexp.MustCompile(`(?s)Could not start the reinstall.*Image not found`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, instancesRoute+"/{id}/reinstall"); n != 1 {
		t.Fatalf("%d reinstall requests, want 1", n)
	}
}

func TestInstanceBackupRestoreAction(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	instanceID := seedBackupInstance(f).ID()
	backup := f.SeedBackup(scope, instanceID, nil)
	f.SeedInstanceTask(scope, instanceID, "apply_firewall")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_backup_restore", fmt.Sprintf(`    instance_id = %q
    backup_id   = %q`, instanceID, backup.ID()), "first")),
				Check: resource.ComposeTestCheckFunc(
					checkTasks(f, fakeapi.TaskRestoreBackup, 1),
					func(*terraform.State) error {
						if n := f.CountRequests(http.MethodPost, backupsRoute+"/{id}/restore"); n < 2 {
							return fmt.Errorf("%d restore requests, want a retry after the busy refusal", n)
						}
						return nil
					},
				),
			},
		},
	})
}

// A backup taken in the same apply is restored once it exists: its id is unknown at plan time.
func TestInstanceBackupRestoreAction_afterBackup(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedBackupInstance(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_instance_backup" "test" {
  instance_id = %[1]q
  name        = "before-test"

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.onetsolutions_instance_backup_restore.test]
    }
  }
}

action "onetsolutions_instance_backup_restore" "test" {
  config {
    instance_id = %[1]q
    backup_id   = onetsolutions_instance_backup.test.id
  }
}
`, instanceID)),
				Check: checkTasks(f, fakeapi.TaskRestoreBackup, 1),
			},
		},
	})
}

func TestInstanceBackupRestoreAction_planChecks(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	instanceID := seedBackupInstance(f).ID()
	pending := f.SeedBackup(scope, instanceID, fakeapi.Object{"status": "running", "size_gb": nil})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		TerraformVersionChecks:   actionVersionChecks,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_backup_restore", fmt.Sprintf(`    instance_id = %q
    backup_id   = %q`, instanceID, pending.ID()), "first")),
				ExpectError: regexp.MustCompile(`(?s)The backup cannot be restored.*is running`),
			},
			{
				Config: acctest.Config(f, triggered("onetsolutions_instance_backup_restore", fmt.Sprintf(`    instance_id = %q
    backup_id   = "9a0b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"`, instanceID), "first")),
				ExpectError: regexp.MustCompile(`Backup not found`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, backupsRoute+"/{id}/restore"); n != 0 {
		t.Fatalf("%d restore requests, want none", n)
	}
}
