package hosting_test

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
	databaseUserName        = "onetsolutions_hosting_database_user.test"
	databaseUserCreateRoute = mailDBHostingRoute + "/database-users/create"
)

func databaseUserConfig(hostingID, username, password string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_database_user" "test" {
  hosting_id = %q
  username   = %q
  password   = %q
}
`, hostingID, username, password)
}

// mailDBCheckField asserts a field of an object of the fake.
func mailDBCheckField(f *fakeapi.Server, kind fakeapi.Kind, hostingID, key, field string, want any) func(*terraform.State) error {
	return func(*terraform.State) error {
		object, ok := f.Get(kind, fakeapi.HostingObjectID(hostingID, key))
		if !ok {
			return fmt.Errorf("%s %s not in the fake API", kind, key)
		}
		if object[field] != want {
			return fmt.Errorf("%s %s has %s = %v, want %v", kind, key, field, object[field], want)
		}
		return nil
	}
}

func TestDatabaseUserResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingDatabaseUser),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, databaseUserConfig(hostingID, "app", mailDBStrongPassword)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(databaseUserName, tfjsonpath.New("id"), knownvalue.StringExact("acme_app")),
					statecheck.ExpectKnownValue(databaseUserName, tfjsonpath.New("full_username"), knownvalue.StringExact("acme_app")),
					statecheck.ExpectSensitiveValue(databaseUserName, tfjsonpath.New("password")),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: mailDBCheckField(f, fakeapi.KindHostingDatabaseUser, hostingID, "acme_app", "password", mailDBStrongPassword),
			},
			{
				ResourceName:            databaseUserName,
				ImportState:             true,
				ImportStateIdFunc:       mailDBImportID(databaseUserName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				// The password cannot be read back: an import plans to record it, without recreating the user.
				ResourceName:       databaseUserName,
				ImportState:        true,
				ImportStateKind:    resource.ImportBlockWithID,
				ImportStateIdFunc:  mailDBImportID(databaseUserName),
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(databaseUserName, plancheck.ResourceActionUpdate)},
				},
			},
			{
				Config:           acctest.Config(f, databaseUserConfig(hostingID, "acme_app", mailDBStrongPassword)),
				ConfigPlanChecks: mailDBExpect(databaseUserName, plancheck.ResourceActionUpdate),
				Check:            mailDBCheckRequests(f, http.MethodPost, databaseUserCreateRoute, 1),
			},
			{
				// No password change in place: a new password recreates the user.
				Config:           acctest.Config(f, databaseUserConfig(hostingID, "acme_app", mailDBStrongPassword2)),
				ConfigPlanChecks: mailDBExpect(databaseUserName, plancheck.ResourceActionDestroyBeforeCreate),
				Check:            mailDBCheckField(f, fakeapi.KindHostingDatabaseUser, hostingID, "acme_app", "password", mailDBStrongPassword2),
			},
			{
				Config:           acctest.Config(f, databaseUserConfig(hostingID, "reports", mailDBStrongPassword2)),
				ConfigPlanChecks: mailDBExpect(databaseUserName, plancheck.ResourceActionDestroyBeforeCreate),
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckExists(f, fakeapi.KindHostingDatabaseUser, hostingID, "acme_app", false),
					mailDBCheckExists(f, fakeapi.KindHostingDatabaseUser, hostingID, "acme_reports", true),
				),
			},
			{
				PreConfig:        func() { f.Delete(fakeapi.KindHostingDatabaseUser, fakeapi.HostingObjectID(hostingID, "acme_reports")) },
				Config:           acctest.Config(f, databaseUserConfig(hostingID, "reports", mailDBStrongPassword2)),
				ConfigPlanChecks: mailDBExpect(databaseUserName, plancheck.ResourceActionCreate),
			},
		},
	})
}

func TestDatabaseUserResource_weakPassword(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      acctest.Config(f, databaseUserConfig(hostingID, "app", "password")),
			ExpectError: regexp.MustCompile(`Weak password`),
		}},
	})
}
