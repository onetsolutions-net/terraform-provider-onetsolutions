package hosting_test

import (
	"fmt"
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

const privilegesName = "onetsolutions_hosting_database_privileges.test"

func privilegesConfig(hostingID, password, privileges, lifecycle string, withGrant bool) string {
	config := fmt.Sprintf(`
resource "onetsolutions_hosting_database" "shop" {
  hosting_id = %[1]q
  name       = "shop"
}

resource "onetsolutions_hosting_database_user" "app" {
  hosting_id = %[1]q
  username   = "app"
  password   = %[2]q
}
`, hostingID, password)
	if !withGrant {
		return config
	}
	return config + fmt.Sprintf(`
resource "onetsolutions_hosting_database_privileges" "test" {
  hosting_id = %q
  database   = onetsolutions_hosting_database.shop.name
  username   = onetsolutions_hosting_database_user.app.username
%s
%s
}
`, hostingID, privileges, lifecycle)
}

// mailDBCheckGrant asserts the privileges the fake holds for acme_app on acme_shop, "" for none.
func mailDBCheckGrant(f *fakeapi.Server, hostingID, want string) func(*terraform.State) error {
	return func(*terraform.State) error {
		db, ok := f.Get(fakeapi.KindHostingDatabase, fakeapi.HostingObjectID(hostingID, "acme_shop"))
		if !ok {
			return fmt.Errorf("database acme_shop not in the fake API")
		}
		got, _ := db.Map("privileges")["acme_app"].(string)
		if got != want {
			return fmt.Errorf("acme_app has %q on acme_shop, want %q", got, want)
		}
		return nil
	}
}

func TestDatabasePrivilegesResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	replaceWithUser := `  lifecycle {
    replace_triggered_by = [onetsolutions_hosting_database_user.app]
  }`
	selectInsert := `  privileges = ["SELECT", "INSERT"]`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingDatabase),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, privilegesConfig(hostingID, mailDBStrongPassword, "", "", true)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(privilegesName, tfjsonpath.New("id"), knownvalue.StringExact("acme_shop/acme_app")),
					statecheck.ExpectKnownValue(privilegesName, tfjsonpath.New("privileges"),
						knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("ALL PRIVILEGES")})),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: mailDBCheckGrant(f, hostingID, "ALL PRIVILEGES"),
			},
			{
				// The privileges cannot be read back, so an import leaves them out.
				ResourceName:            privilegesName,
				ImportState:             true,
				ImportStateIdFunc:       mailDBImportID(privilegesName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"privileges"},
			},
			{
				ResourceName:            privilegesName,
				ImportState:             true,
				ImportStateId:           hostingID + "/shop/app",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"privileges"},
			},
			{
				ResourceName:       privilegesName,
				ImportState:        true,
				ImportStateKind:    resource.ImportBlockWithID,
				ImportStateIdFunc:  mailDBImportID(privilegesName),
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(privilegesName, plancheck.ResourceActionUpdate)},
				},
			},
			{
				Config:           acctest.Config(f, privilegesConfig(hostingID, mailDBStrongPassword, selectInsert, replaceWithUser, true)),
				ConfigPlanChecks: mailDBExpect(privilegesName, plancheck.ResourceActionUpdate),
				Check:            mailDBCheckGrant(f, hostingID, "INSERT,SELECT"),
			},
			{
				// A new password recreates the user, which drops its privileges; replace_triggered_by grants them again.
				Config:           acctest.Config(f, privilegesConfig(hostingID, mailDBStrongPassword2, selectInsert, replaceWithUser, true)),
				ConfigPlanChecks: mailDBExpect(privilegesName, plancheck.ResourceActionDestroyBeforeCreate),
				Check:            mailDBCheckGrant(f, hostingID, "INSERT,SELECT"),
			},
			{
				// Privileges revoked outside Terraform are drift.
				PreConfig: func() {
					f.Mutate(fakeapi.KindHostingDatabase, fakeapi.HostingObjectID(hostingID, "acme_shop"), func(o fakeapi.Object) {
						o["users"], o["privileges"] = []any{}, fakeapi.Object{}
					})
				},
				Config:           acctest.Config(f, privilegesConfig(hostingID, mailDBStrongPassword2, selectInsert, replaceWithUser, true)),
				ConfigPlanChecks: mailDBExpect(privilegesName, plancheck.ResourceActionCreate),
				Check:            mailDBCheckGrant(f, hostingID, "INSERT,SELECT"),
			},
			{
				// Destroying the resource cannot revoke them: they stay granted.
				Config:           acctest.Config(f, privilegesConfig(hostingID, mailDBStrongPassword2, "", "", false)),
				ConfigPlanChecks: mailDBExpect(privilegesName, plancheck.ResourceActionDestroy),
				Check:            mailDBCheckGrant(f, hostingID, "INSERT,SELECT"),
			},
		},
	})
}

// A database named by its full name, unknown while the database is replaced, replaces the
// privileges too: they are granted on the new database.
func TestDatabasePrivilegesResource_databaseReplaced(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	config := func(name string) string {
		return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_hosting_database" "db" {
  hosting_id = %[1]q
  name       = %[2]q
}

resource "onetsolutions_hosting_database_user" "app" {
  hosting_id = %[1]q
  username   = "app"
  password   = %[3]q
}

resource "onetsolutions_hosting_database_privileges" "test" {
  hosting_id = %[1]q
  database   = onetsolutions_hosting_database.db.full_name
  username   = onetsolutions_hosting_database_user.app.username
}
`, hostingID, name, mailDBStrongPassword))
	}
	granted := func(database string) func(*terraform.State) error {
		return func(*terraform.State) error {
			db, ok := f.Get(fakeapi.KindHostingDatabase, fakeapi.HostingObjectID(hostingID, database))
			if !ok {
				return fmt.Errorf("database %s not in the fake API", database)
			}
			if got, _ := db.Map("privileges")["acme_app"].(string); got != "ALL PRIVILEGES" {
				return fmt.Errorf("acme_app has %q on %s, want ALL PRIVILEGES", got, database)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingDatabase),
		Steps: []resource.TestStep{
			{Config: config("shop"), Check: granted("acme_shop")},
			{
				Config:           config("blog"),
				ConfigPlanChecks: mailDBExpect(privilegesName, plancheck.ResourceActionDestroyBeforeCreate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(privilegesName, tfjsonpath.New("id"), knownvalue.StringExact("acme_blog/acme_app")),
				},
				Check: granted("acme_blog"),
			},
		},
	})
}

func TestDatabasePrivilegesResource_invalidPrivileges(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, privilegesConfig(hostingID, mailDBStrongPassword, `  privileges = ["ALL PRIVILEGES", "SELECT"]`, "", true)),
				ExpectError: regexp.MustCompile(`Conflicting privileges`),
			},
			{
				Config:      acctest.Config(f, privilegesConfig(hostingID, mailDBStrongPassword, `  privileges = ["GRANT OPTION"]`, "", true)),
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}
