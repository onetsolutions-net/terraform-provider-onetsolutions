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
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

const (
	ftpName          = "onetsolutions_hosting_ftp_account.test"
	ftpDefaultName   = "onetsolutions_hosting_ftp_account.web"
	ftpCreateRoute   = mailDBHostingRoute + "/ftp-accounts/create"
	ftpPasswordRoute = mailDBHostingRoute + "/ftp-accounts/{username}/password"
)

func ftpConfig(hostingID, password, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_ftp_account" "test" {
  hosting_id = %[1]q
  username   = "deploy"
  password   = %[2]q
  directory  = "/public_html/app"
%[3]s
}

resource "onetsolutions_hosting_ftp_account" "web" {
  hosting_id = %[1]q
  username   = "web"
  password   = %[2]q
}
`, hostingID, password, extra)
}

func TestFTPAccountResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingFTPAccount),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, ftpConfig(hostingID, mailDBStrongPassword, "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(ftpName, tfjsonpath.New("id"), knownvalue.StringExact("deploy")),
					statecheck.ExpectKnownValue(ftpName, tfjsonpath.New("login"), knownvalue.StringExact("deploy@acme-site.fr")),
					statecheck.ExpectKnownValue(ftpName, tfjsonpath.New("home_directory"), knownvalue.StringExact("/home/acme/public_html/app")),
					statecheck.ExpectKnownValue(ftpName, tfjsonpath.New("quota_mb"), knownvalue.Int64Exact(0)),
					statecheck.ExpectKnownValue(ftpDefaultName, tfjsonpath.New("directory"), knownvalue.StringExact("/public_html")),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: mailDBCheckField(f, fakeapi.KindHostingFTPAccount, hostingID, "deploy", "password", mailDBStrongPassword),
			},
			{
				ResourceName:            ftpName,
				ImportState:             true,
				ImportStateIdFunc:       mailDBImportID(ftpName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				ResourceName:            ftpDefaultName,
				ImportState:             true,
				ImportStateIdFunc:       mailDBImportID(ftpDefaultName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				ResourceName:       ftpName,
				ImportState:        true,
				ImportStateKind:    resource.ImportBlockWithID,
				ImportStateIdFunc:  mailDBImportID(ftpName),
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(ftpName, plancheck.ResourceActionUpdate)},
				},
			},
			{
				Config:           acctest.Config(f, ftpConfig(hostingID, mailDBStrongPassword2, "")),
				ConfigPlanChecks: mailDBExpect(ftpName, plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckField(f, fakeapi.KindHostingFTPAccount, hostingID, "deploy", "password", mailDBStrongPassword2),
					mailDBCheckRequests(f, http.MethodPost, ftpPasswordRoute, 2),
					mailDBCheckRequests(f, http.MethodPost, ftpCreateRoute, 2),
				),
			},
			{
				// The quota cannot change in place: the account is recreated, its files stay.
				Config:           acctest.Config(f, ftpConfig(hostingID, mailDBStrongPassword2, "  quota_mb = 2048")),
				ConfigPlanChecks: mailDBExpect(ftpName, plancheck.ResourceActionDestroyBeforeCreate),
				Check:            mailDBCheckField(f, fakeapi.KindHostingFTPAccount, hostingID, "deploy", "quota_mb", float64(2048)),
			},
			{
				PreConfig:        func() { f.Delete(fakeapi.KindHostingFTPAccount, fakeapi.HostingObjectID(hostingID, "deploy")) },
				Config:           acctest.Config(f, ftpConfig(hostingID, mailDBStrongPassword2, "  quota_mb = 2048")),
				ConfigPlanChecks: mailDBExpect(ftpName, plancheck.ResourceActionCreate),
			},
		},
	})
}

func TestFTPAccountResource_invalidDirectory(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	config := func(directory string) string {
		return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_hosting_ftp_account" "test" {
  hosting_id = %q
  username   = "deploy"
  password   = %q
  directory  = %q
}
`, hostingID, mailDBStrongPassword, directory))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("public_html"), ExpectError: regexp.MustCompile(`must start with /`)},
			{Config: config("/public_html/"), ExpectError: regexp.MustCompile(`must start with /`)},
			{Config: config("/public_html/../etc"), ExpectError: regexp.MustCompile(`must not contain . or .. segments`)},
		},
	})
}
