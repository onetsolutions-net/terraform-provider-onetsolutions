package backup_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

func TestInstanceBackupsDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	instanceID := seedBackupInstance(f).ID()
	older := f.SeedBackup(scope, instanceID, fakeapi.Object{"name": "nightly-1", "backup_type": "scheduled"})
	f.SeedBackup(scope, instanceID, fakeapi.Object{"name": "expired", "status": "deleted"})
	newer := f.SeedBackup(scope, instanceID, fakeapi.Object{"name": "before-upgrade", "retention_days": 3})
	empty := seedBackupInstance(f).ID()
	const all, done, none = "data.onetsolutions_instance_backups.all", "data.onetsolutions_instance_backups.done", "data.onetsolutions_instance_backups.none"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, `
data "onetsolutions_instance_backups" "x" {
  instance_id = "9a0b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d"
}
`),
				ExpectError: regexp.MustCompile(`Could not list the backups`),
			},
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_instance_backups" "x" {
  instance_id = %q
  status      = "done"
}
`, instanceID)),
				ExpectError: regexp.MustCompile(`(?s)status.*value must be one of`),
			},
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_instance_backups" "all" {
  instance_id = %[1]q
}
data "onetsolutions_instance_backups" "done" {
  instance_id = %[1]q
  status      = "completed"
}
data "onetsolutions_instance_backups" "none" {
  instance_id = %[2]q
}
`, instanceID, empty)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(all, tfjsonpath.New("backups"), knownvalue.ListSizeExact(3)),
					statecheck.ExpectKnownValue(all, tfjsonpath.New("backups").AtSliceIndex(0).AtMapKey("id"), knownvalue.StringExact(newer.ID())),
					statecheck.ExpectKnownValue(all, tfjsonpath.New("backups").AtSliceIndex(0).AtMapKey("retention_days"), knownvalue.Int64Exact(3)),
					statecheck.ExpectKnownValue(all, tfjsonpath.New("backups").AtSliceIndex(1).AtMapKey("status"), knownvalue.StringExact("deleted")),
					statecheck.ExpectKnownValue(all, tfjsonpath.New("backups").AtSliceIndex(2).AtMapKey("backup_type"), knownvalue.StringExact("scheduled")),
					statecheck.ExpectKnownValue(all, tfjsonpath.New("backups").AtSliceIndex(2).AtMapKey("size_gb"), knownvalue.StringExact("4.10")),
					statecheck.ExpectKnownValue(all, tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
					statecheck.ExpectKnownValue(done, tfjsonpath.New("backups"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue(done, tfjsonpath.New("backups").AtSliceIndex(1).AtMapKey("name"), knownvalue.StringExact(older.String("name"))),
					statecheck.ExpectKnownValue(none, tfjsonpath.New("backups"), knownvalue.ListSizeExact(0)),
				},
			},
		},
	})
}
