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
	emailName          = "onetsolutions_hosting_email_account.test"
	emailCreateRoute   = mailDBHostingRoute + "/emails/create"
	emailDeleteRoute   = mailDBHostingRoute + "/emails/{email}/delete"
	emailPasswordRoute = mailDBHostingRoute + "/emails/{email}/password"
	emailQuotaRoute    = mailDBHostingRoute + "/emails/{email}/quota"
	mailDBContactMail  = "contact@acme-site.fr"
	mailDBSalesMail    = "sales@acme-site.fr"
	mailDBMainMailbox  = "acme@acme-site.fr"
)

func emailConfig(hostingID, email, password, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_email_account" "test" {
  hosting_id = %q
  email      = %q
  password   = %q
%s
}
`, hostingID, email, password, extra)
}

func TestEmailAccountResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingEmail),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, emailConfig(hostingID, mailDBContactMail, mailDBStrongPassword, "")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(emailName, tfjsonpath.New("id"), knownvalue.StringExact(mailDBContactMail)),
					statecheck.ExpectKnownValue(emailName, tfjsonpath.New("quota_mb"), knownvalue.Int64Exact(250)),
					statecheck.ExpectKnownValue(emailName, tfjsonpath.New("user"), knownvalue.StringExact("contact")),
					statecheck.ExpectKnownValue(emailName, tfjsonpath.New("domain"), knownvalue.StringExact("acme-site.fr")),
					statecheck.ExpectKnownValue(emailName, tfjsonpath.New("is_main"), knownvalue.Bool(false)),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: mailDBCheckField(f, fakeapi.KindHostingEmail, hostingID, mailDBContactMail, "password", mailDBStrongPassword),
			},
			{
				ResourceName:            emailName,
				ImportState:             true,
				ImportStateIdFunc:       mailDBImportID(emailName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				// The password cannot be read back: after an import, the next apply sets it.
				ResourceName:       emailName,
				ImportState:        true,
				ImportStateKind:    resource.ImportBlockWithID,
				ImportStateIdFunc:  mailDBImportID(emailName),
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(emailName, plancheck.ResourceActionUpdate)},
				},
			},
			{
				Config:           acctest.Config(f, emailConfig(hostingID, mailDBContactMail, mailDBStrongPassword2, "  quota_mb = 1024")),
				ConfigPlanChecks: mailDBExpect(emailName, plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckField(f, fakeapi.KindHostingEmail, hostingID, mailDBContactMail, "password", mailDBStrongPassword2),
					mailDBCheckField(f, fakeapi.KindHostingEmail, hostingID, mailDBContactMail, "quota_mb", float64(1024)),
					mailDBCheckRequests(f, http.MethodPost, emailPasswordRoute, 1),
					mailDBCheckRequests(f, http.MethodPost, emailQuotaRoute, 1),
					mailDBCheckRequests(f, http.MethodPost, emailCreateRoute, 1),
				),
			},
			{
				// A quota changed outside Terraform is drift, set back in place.
				PreConfig: func() {
					f.Mutate(fakeapi.KindHostingEmail, fakeapi.HostingObjectID(hostingID, mailDBContactMail), func(o fakeapi.Object) {
						o["quota_mb"] = 100
					})
				},
				Config:           acctest.Config(f, emailConfig(hostingID, mailDBContactMail, mailDBStrongPassword2, "  quota_mb = 1024")),
				ConfigPlanChecks: mailDBExpect(emailName, plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckField(f, fakeapi.KindHostingEmail, hostingID, mailDBContactMail, "quota_mb", float64(1024)),
					mailDBCheckRequests(f, http.MethodPost, emailPasswordRoute, 1),
				),
			},
			{
				Config:           acctest.Config(f, emailConfig(hostingID, mailDBSalesMail, mailDBStrongPassword2, "  quota_mb = 0")),
				ConfigPlanChecks: mailDBExpect(emailName, plancheck.ResourceActionDestroyBeforeCreate),
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckExists(f, fakeapi.KindHostingEmail, hostingID, mailDBContactMail, false),
					mailDBCheckField(f, fakeapi.KindHostingEmail, hostingID, mailDBSalesMail, "quota_mb", float64(0)),
				),
			},
			{
				PreConfig:        func() { f.Delete(fakeapi.KindHostingEmail, fakeapi.HostingObjectID(hostingID, mailDBSalesMail)) },
				Config:           acctest.Config(f, emailConfig(hostingID, mailDBSalesMail, mailDBStrongPassword2, "  quota_mb = 0")),
				ConfigPlanChecks: mailDBExpect(emailName, plancheck.ResourceActionCreate),
			},
		},
	})
}

// The main mailbox belongs to the web hosting: it can be imported to manage its password and
// quota, and destroying it only forgets it.
func TestEmailAccountResource_mainMailbox(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	importBlock := fmt.Sprintf(`
import {
  to = onetsolutions_hosting_email_account.test
  id = "%s/%s"
}
`, hostingID, mailDBMainMailbox)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckRequests(f, http.MethodDelete, emailDeleteRoute, 0),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, importBlock, emailConfig(hostingID, mailDBMainMailbox, mailDBStrongPassword, "  quota_mb = 0")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(emailName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(emailName, tfjsonpath.New("is_main"), knownvalue.Bool(true)),
				},
				Check: resource.ComposeTestCheckFunc(
					mailDBCheckField(f, fakeapi.KindHostingEmail, hostingID, mailDBMainMailbox, "password", mailDBStrongPassword),
					mailDBCheckRequests(f, http.MethodPost, emailCreateRoute, 0),
				),
			},
		},
	})
}

// Without quota_mb, the main mailbox keeps its quota instead of getting the default.
func TestEmailAccountResource_mainMailboxQuotaUnset(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	importBlock := fmt.Sprintf(`
import {
  to = onetsolutions_hosting_email_account.test
  id = "%s/%s"
}
`, hostingID, mailDBMainMailbox)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, importBlock, emailConfig(hostingID, mailDBMainMailbox, mailDBStrongPassword, "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(emailName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(emailName, tfjsonpath.New("quota_mb"), knownvalue.Int64Exact(0)),
				},
				Check: mailDBCheckRequests(f, http.MethodPost, emailQuotaRoute, 0),
			},
		},
	})
}

// The API may fail to answer a create that took effect: the mailbox is then adopted.
func TestEmailAccountResource_createLostAnswer(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()
	f.Inject(http.MethodPost, emailCreateRoute, fakeapi.Injection{Status: http.StatusInternalServerError, AfterHandling: true})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             mailDBCheckNoneLeft(f, fakeapi.KindHostingEmail),
		Steps: []resource.TestStep{{
			Config: acctest.Config(f, emailConfig(hostingID, mailDBContactMail, mailDBStrongPassword, "")),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			Check: mailDBCheckRequests(f, http.MethodPost, emailCreateRoute, 1),
		}},
	})
}

func TestEmailAccountResource_refusals(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := mailDBHosting(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, emailConfig(hostingID, "Contact@acme-site.fr", mailDBStrongPassword, "")),
				ExpectError: regexp.MustCompile(`must be a lowercase email address`),
			},
			{
				Config:      acctest.Config(f, emailConfig(hostingID, mailDBContactMail, mailDBStrongPassword, "  quota_mb = -1")),
				ExpectError: regexp.MustCompile(`must be at least 0`),
			},
			{
				Config:      acctest.Config(f, emailConfig(hostingID, "contact@elsewhere.fr", mailDBStrongPassword, "")),
				ExpectError: regexp.MustCompile(`Could not create the mailbox`),
			},
		},
	})
}
