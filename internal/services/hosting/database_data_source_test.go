package hosting_test

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

func TestMailDBDataSources(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	hosting := mailDBHosting(f)
	hostingID := hosting.ID()
	f.SeedHostingDatabase(scope, hosting, "shop")
	f.SeedHostingDatabaseUser(scope, hosting, "app", mailDBStrongPassword)
	f.Mutate(fakeapi.KindHostingDatabase, fakeapi.HostingObjectID(hostingID, "acme_shop"), func(o fakeapi.Object) {
		o["users"] = []any{"acme_app"}
	})
	f.SeedHostingEmail(scope, hosting, mailDBContactMail, 500)
	f.SeedHostingFTPAccount(scope, hosting, "deploy", "/public_html/app", 1024)

	databases := "data.onetsolutions_hosting_databases.all"
	users := "data.onetsolutions_hosting_database_users.all"
	mailboxes := "data.onetsolutions_hosting_email_accounts.all"
	ftp := "data.onetsolutions_hosting_ftp_accounts.all"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_hosting_databases" "all" {
  hosting_id = %[1]q
}
data "onetsolutions_hosting_database_users" "all" {
  hosting_id = %[1]q
}
data "onetsolutions_hosting_email_accounts" "all" {
  hosting_id = %[1]q
}
data "onetsolutions_hosting_ftp_accounts" "all" {
  hosting_id = %[1]q
}
`, hostingID)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(databases, tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
					statecheck.ExpectKnownValue(databases, tfjsonpath.New("databases"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{
							"name":    knownvalue.StringExact("acme_shop"),
							"size_mb": knownvalue.Float64Exact(1.5),
							"users":   knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("acme_app")}),
						}),
					})),
					statecheck.ExpectKnownValue(users, tfjsonpath.New("database_users").AtSliceIndex(0).AtMapKey("username"),
						knownvalue.StringExact("acme_app")),
					statecheck.ExpectKnownValue(mailboxes, tfjsonpath.New("email_accounts"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue(mailboxes, tfjsonpath.New("email_accounts").AtSliceIndex(0), knownvalue.ObjectExact(map[string]knownvalue.Check{
						"email": knownvalue.StringExact(mailDBMainMailbox), "user": knownvalue.StringExact("acme"),
						"domain": knownvalue.StringExact("acme-site.fr"), "quota_mb": knownvalue.Int64Exact(0),
						"quota_used_mb": knownvalue.Int64Exact(0), "is_main": knownvalue.Bool(true),
					})),
					statecheck.ExpectKnownValue(mailboxes, tfjsonpath.New("email_accounts").AtSliceIndex(1), knownvalue.ObjectExact(map[string]knownvalue.Check{
						"email": knownvalue.StringExact(mailDBContactMail), "user": knownvalue.StringExact("contact"),
						"domain": knownvalue.StringExact("acme-site.fr"), "quota_mb": knownvalue.Int64Exact(500),
						"quota_used_mb": knownvalue.Int64Exact(3), "is_main": knownvalue.Bool(false),
					})),
					statecheck.ExpectKnownValue(ftp, tfjsonpath.New("ftp_accounts"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue(ftp, tfjsonpath.New("ftp_accounts").AtSliceIndex(1), knownvalue.ObjectExact(map[string]knownvalue.Check{
						"username": knownvalue.StringExact("deploy"), "home_directory": knownvalue.StringExact("/home/acme/public_html/app"),
						"quota_mb": knownvalue.Int64Exact(1024), "quota_used_mb": knownvalue.Int64Exact(0),
					})),
				},
			},
			{
				Config: acctest.Config(f, `
data "onetsolutions_hosting_databases" "missing" {
  hosting_id = "3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b"
}
`),
				ExpectError: regexp.MustCompile(`Could not list the databases`),
			},
		},
	})
}
