package backup_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

func seedBackupInstance(f *fakeapi.Server) fakeapi.Object {
	return f.SeedInstance(f.Scope(), fakeapi.Object{"backup_enabled": true})
}

func captureID(id *string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[backupName]
		if !ok {
			return fmt.Errorf("%s not in state", backupName)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func checkIDChanged(previous *string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[backupName]
		if !ok {
			return fmt.Errorf("%s not in state", backupName)
		}
		if rs.Primary.ID == *previous {
			return fmt.Errorf("%s kept id %s, expected a new backup", backupName, *previous)
		}
		*previous = rs.Primary.ID
		return nil
	}
}

// checkBackupsKept is the CheckDestroy of onetsolutions_instance_backup: a destroy only forgets a
// backup, so every backup the state held must still exist in the fake.
func checkBackupsKept(f *fakeapi.Server) func(*terraform.State) error {
	return func(state *terraform.State) error {
		for _, rs := range state.RootModule().Resources {
			if rs.Type == "onetsolutions_instance_backup" && !f.Exists(fakeapi.KindBackup, rs.Primary.ID) {
				return fmt.Errorf("backup %s was removed from the fake API: a destroy must keep it", rs.Primary.ID)
			}
		}
		return nil
	}
}

// actionsSupported skips a test below Terraform 1.14 and under OpenTofu, which do not run
// provider actions.
type actionsSupported struct{}

func (actionsSupported) CheckTerraformVersion(ctx context.Context, req tfversion.CheckTerraformVersionRequest, resp *tfversion.CheckTerraformVersionResponse) {
	binary := filepath.Base(os.Getenv("TF_ACC_TERRAFORM_PATH"))
	if strings.HasPrefix(binary, "tofu") || os.Getenv("TF_ACC_PROVIDER_HOST") == "registry.opentofu.org" {
		resp.Skip = "OpenTofu does not run provider actions"
		return
	}
	var below tfversion.CheckTerraformVersionResponse
	tfversion.SkipBelow(tfversion.Version1_14_0).CheckTerraformVersion(ctx, req, &below)
	resp.Skip, resp.Error = below.Skip, below.Error
}

var actionVersionChecks = []tfversion.TerraformVersionCheck{actionsSupported{}}
