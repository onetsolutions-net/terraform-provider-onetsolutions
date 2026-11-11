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
	mailDBHostingRoute    = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}"
	databaseName          = "onetsolutions_hosting_database.test"
	databaseCreateRoute   = mailDBHostingRoute + "/databases/create"
	databaseDeleteRoute   = mailDBHostingRoute + "/databases/{name}/delete"
	mailDBStrongPassword  = "Initial-Pass1"
	mailDBStrongPassword2 = "Rotated-Pass2"
)

// mailDBHosting seeds an active web hosting of username "acme" on its own domain.
func mailDBHosting(f *fakeapi.Server) fakeapi.Object {
	return f.SeedWebHosting(f.Scope(), fakeapi.Object{"username": "acme", "domain": "acme-site.fr"})
}

func databaseConfig(hostingID, name, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_database" "test" {
  hosting_id = %q
  name       = %q
%s
}
`, hostingID, name, extra)
}

// mailDBCheckNoneLeft asserts no object of kind remains in the fake.
func mailDBCheckNoneLeft(f *fakeapi.Server, kind fakeapi.Kind) func(*terraform.State) error {
	return func(*terraform.State) error {
		if left := f.All(kind); len(left) > 0 {
			return fmt.Errorf("%d %s objects left in the fake API: %v", len(left), kind, left)
		}
		return nil
	}
}

func mailDBCheckExists(f *fakeapi.Server, kind fakeapi.Kind, hostingID, key string, want bool) func(*terraform.State) error {
	return func(*terraform.State) error {
		if got := f.Exists(kind, fakeapi.HostingObjectID(hostingID, key)); got != want {
			return fmt.Errorf("%s %s exists = %t, want %t", kind, key, got, want)
		}
		return nil
	}
}

func mailDBCheckRequests(f *fakeapi.Server, method, template string, want int) func(*terraform.State) error {
	return func(*terraform.State) error {
		if got := f.CountRequests(method, template); got != want {
			return fmt.Errorf("%s %s sent %d times, want %d", method, template, got, want)
		}
		return nil
	}
}

func mailDBImportID(name string) func(*terraform.State) (string, error) {
	return acctest.ChildImportIDFunc(name, "hosting_id")
}

func mailDBExpect(name string, action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, action)},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func TestDatabaseResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingDatabase),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, databaseConfig(hostingID, "shop", "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(databaseName, tfjsonpath.New("id"), knownvalue.StringExact("acme_shop")),
					statecheck.ExpectKnownValue(databaseName, tfjsonpath.New("full_name"), knownvalue.StringExact("acme_shop")),
					statecheck.ExpectKnownValue(databaseName, tfjsonpath.New("name"), knownvalue.StringExact("shop")),
					statecheck.ExpectKnownValue(databaseName, tfjsonpath.New("deletion_protection"), knownvalue.Bool(false)),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: mailDBCheckExists(f, fakeapi.KindHostingDatabase, hostingID, "acme_shop", true),
			},
			{
				ResourceName:      databaseName,
				ImportState:       true,
				ImportStateIdFunc: mailDBImportID(databaseName),
				ImportStateVerify: true,
			},
			{
				// The short form, with the full name: the import writes the name without its prefix.
				ResourceName:      databaseName,
				ImportState:       true,
				ImportStateId:     hostingID + "/acme_shop",
				ImportStateVerify: true,
			},
			{
				ResourceName:      databaseName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: mailDBImportID(databaseName),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(databaseName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// The prefixed spelling of the same database is an in-place change, not a new database.
				Config:           acctest.Config(f, databaseConfig(hostingID, "acme_shop", "")),
				ConfigPlanChecks: mailDBExpect(databaseName, plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckRequests(f, http.MethodPost, databaseCreateRoute, 1),
					resource.TestCheckResourceAttr(databaseName, "full_name", "acme_shop"),
				),
			},
			{
				Config:           acctest.Config(f, databaseConfig(hostingID, "blog", "")),
				ConfigPlanChecks: mailDBExpect(databaseName, plancheck.ResourceActionDestroyBeforeCreate),
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckExists(f, fakeapi.KindHostingDatabase, hostingID, "acme_shop", false),
					mailDBCheckExists(f, fakeapi.KindHostingDatabase, hostingID, "acme_blog", true),
					resource.TestCheckResourceAttr(databaseName, "id", "acme_blog"),
				),
			},
			{
				PreConfig:        func() { f.Delete(fakeapi.KindHostingDatabase, fakeapi.HostingObjectID(hostingID, "acme_blog")) },
				Config:           acctest.Config(f, databaseConfig(hostingID, "blog", "")),
				ConfigPlanChecks: mailDBExpect(databaseName, plancheck.ResourceActionCreate),
				Check:            mailDBCheckExists(f, fakeapi.KindHostingDatabase, hostingID, "acme_blog", true),
			},
		},
	})
}

func TestDatabaseResource_deletionProtection(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	protected := acctest.Config(f, databaseConfig(hostingID, "shop", "  deletion_protection = true"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingDatabase),
		Steps: []resource.TestStep{
			{Config: protected},
			{Config: protected, Destroy: true, ExpectError: regexp.MustCompile(`The database is protected from deletion`)},
			{
				Config:      acctest.Config(f, databaseConfig(hostingID, "blog", "  deletion_protection = true")),
				ExpectError: regexp.MustCompile(`The database is protected from deletion`),
			},
			{
				Config:           acctest.Config(f, databaseConfig(hostingID, "shop", "")),
				ConfigPlanChecks: mailDBExpect(databaseName, plancheck.ResourceActionUpdate),
				Check:            mailDBCheckRequests(f, http.MethodDelete, databaseDeleteRoute, 0),
			},
		},
	})
}

// A delete the API answers with a failure after it took effect, or that finds the database
// already gone, succeeds once the database is no longer listed.
func TestDatabaseResource_deleteFailureAfterEffect(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	f.Inject(http.MethodDelete, databaseDeleteRoute, fakeapi.Injection{
		Status: http.StatusBadRequest, AfterHandling: true,
		Body: fakeapi.Object{"detail": "Operation failed. Please try again later.", "code": "hosting_operation_failed"},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingDatabase),
		Steps:                    []resource.TestStep{{Config: acctest.Config(f, databaseConfig(hostingID, "shop", ""))}},
	})
}

// A create that fails without a clear answer is adopted when the database exists afterwards.
func TestDatabaseResource_createLostAnswer(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	f.Inject(http.MethodPost, databaseCreateRoute, fakeapi.Injection{Status: http.StatusInternalServerError, AfterHandling: true})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingDatabase),
		Steps: []resource.TestStep{{
			Config: acctest.Config(f, databaseConfig(hostingID, "shop", "")),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			Check: resource.TestCheckResourceAttr(databaseName, "id", "acme_shop"),
		}},
	})
}

func TestDatabaseResource_refusals(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	f.SeedHostingDatabase(f.Scope(), mailDBMustHosting(t, f, hostingID), "taken")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, databaseConfig(hostingID, "my-shop", "")),
				ExpectError: regexp.MustCompile(`only letters, digits and underscores`),
			},
			{
				Config:      acctest.Config(f, databaseConfig(hostingID, "a_name_far_too_long", "")),
				ExpectError: regexp.MustCompile(`length must be between 1 and 16`),
			},
			{
				Config:      acctest.Config(f, databaseConfig(hostingID, "taken", "")),
				ExpectError: regexp.MustCompile(`Could not create the database(?s:.*)Operation failed`),
			},
			{
				ResourceName:  databaseName,
				Config:        acctest.Config(f, databaseConfig(hostingID, "taken", "")),
				ImportState:   true,
				ImportStateId: "a/b/c",
				ExpectError:   regexp.MustCompile(`expected\s+<organization_id>/<project_id>/<hosting_id>/<name>`),
			},
		},
	})
}

// When the web hosting ends, what it held is gone: a refresh forgets it and a destroy only
// removes it from the state.
func TestDatabaseResource_hostingEnded(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	config := acctest.Config(f, databaseConfig(hostingID, "shop", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindWebHosting, hostingID, func(o fakeapi.Object) {
						o["status"], o["is_active"] = "terminated", false
					})
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
		CheckDestroy: mailDBCheckRequests(f, http.MethodDelete, databaseDeleteRoute, 1),
	})
}

func mailDBMustHosting(t *testing.T, f *fakeapi.Server, id string) fakeapi.Object {
	t.Helper()
	hosting, ok := f.Get(fakeapi.KindWebHosting, id)
	if !ok {
		t.Fatalf("web hosting %s not in the fake", id)
	}
	return hosting
}
