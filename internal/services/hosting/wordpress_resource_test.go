package hosting_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

const (
	wordPressName   = "onetsolutions_hosting_wordpress.test"
	updateWPRoute   = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/wordpress/{instance_id}/update"
	installWPRoute  = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/wordpress/install"
	wordPressAdmins = `  admin_user     = "admin"
  admin_password = "Correct-Horse-9"
  admin_email    = "admin@example.fr"`
)

func wordPressConfig(f *fakeapi.Server, hostingID, domain, body string) string {
	return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_hosting_wordpress" "test" {
  hosting_id = %q
  domain     = %q
%s
}
`, hostingID, domain, body))
}

func wpActions(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(wordPressName, action)},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func TestWordPressResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-wp.fr"})
	f.SeedHostingDomain(hosting.ID(), "blog.tf-acc-wp.fr")
	id := hosting.ID()
	var siteID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_hosting_wordpress", fakeapi.KindWordPressSite),
		Steps: []resource.TestStep{
			{
				Config:           wordPressConfig(f, id, "tf-acc-wp.fr", wordPressAdmins+"\n  title = \"My site\"\n  language = \"fr\""),
				ConfigPlanChecks: wpActions(plancheck.ResourceActionCreate),
				Check: resource.ComposeTestCheckFunc(
					captureID(wordPressName, &siteID),
					resource.TestCheckResourceAttr(wordPressName, "path", ""),
					resource.TestCheckResourceAttr(wordPressName, "version", fakeapi.WordPressLatestVersion),
					resource.TestCheckResourceAttr(wordPressName, "deletion_protection", "false"),
				),
			},
			{
				ResourceName:            wordPressName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ChildImportIDFunc(wordPressName, "hosting_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"admin_user", "admin_password", "admin_email", "title", "language"},
			},
			{
				// A changed password is stored with a warning: it does not change the site.
				Config: wordPressConfig(f, id, "tf-acc-wp.fr", strings.Replace(wordPressAdmins, "Correct-Horse-9",
					"Another-Long-Password-7", 1)+"\n  title = \"My site\"\n  language = \"fr\""),
				ConfigPlanChecks: wpActions(plancheck.ResourceActionUpdate),
				Check:            checkIDUnchanged(wordPressName, &siteID),
			},
			{
				// Outdated outside Terraform, then updated by a new trigger, in place.
				PreConfig: func() {
					f.Mutate(fakeapi.KindWordPressSite, siteID, func(o fakeapi.Object) { o["version"] = "6.4.0" })
				},
				Config: wordPressConfig(f, id, "tf-acc-wp.fr", wordPressAdmins+"\n  title = \"My site\"\n  language = \"fr\""+
					"\n  update_trigger = \"2026-11\""),
				ConfigPlanChecks: wpActions(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(wordPressName, &siteID),
					resource.TestCheckResourceAttr(wordPressName, "version", fakeapi.WordPressLatestVersion),
				),
			},
			{
				// Another path is another site.
				Config:           wordPressConfig(f, id, "blog.tf-acc-wp.fr", wordPressAdmins+"\n  path = \"news\""),
				ConfigPlanChecks: wpActions(plancheck.ResourceActionDestroyBeforeCreate),
				Check: func(*terraform.State) error {
					if f.Exists(fakeapi.KindWordPressSite, siteID) {
						return fmt.Errorf("the replaced site %s still exists", siteID)
					}
					return nil
				},
			},
			{
				// Deleted outside Terraform: the next plan installs it again.
				PreConfig: func() {
					for _, site := range f.All(fakeapi.KindWordPressSite) {
						f.Delete(fakeapi.KindWordPressSite, site.ID())
					}
				},
				Config:           wordPressConfig(f, id, "blog.tf-acc-wp.fr", wordPressAdmins+"\n  path = \"news\""),
				ConfigPlanChecks: wpActions(plancheck.ResourceActionCreate),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, updateWPRoute); n != 1 {
		t.Fatalf("%d updates, want 1", n)
	}
}

func TestWordPressResource_importPlansNothing(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-wpimport.fr"})
	site := f.SeedWordPressSite(f.Scope(), hosting.ID(), fakeapi.Object{"path": "shop"})
	config := wordPressConfig(f, hosting.ID(), "tf-acc-wpimport.fr", `  path = "shop"`)
	importID := hosting.ID() + "/" + site.ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:          config,
				ResourceName:    wordPressName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   importID,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(wordPressName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config:             config,
				ResourceName:       wordPressName,
				ImportState:        true,
				ImportStateId:      importID,
				ImportStatePersist: true,
			},
			{
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr(wordPressName, "version", "6.5.0"),
			},
			{
				// Installation inputs added after an import are stored, nothing is reinstalled.
				Config:           wordPressConfig(f, hosting.ID(), "tf-acc-wpimport.fr", "  path = \"shop\"\n"+wordPressAdmins),
				ConfigPlanChecks: wpActions(plancheck.ResourceActionUpdate),
				Check:            resource.TestCheckResourceAttr(wordPressName, "id", site.ID()),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, installWPRoute); n != 0 {
		t.Fatalf("%d installs, want 0", n)
	}
}

func TestWordPressResource_checks(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-wpcheck.fr"})
	pending := f.SeedWebHosting(f.Scope(), fakeapi.Object{"status": "pending"})
	f.SetHostingPHPVersion(hosting.ID(), "tf-acc-wpcheck.fr", "ea-php74")
	id := hosting.ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      wordPressConfig(f, id, "tf-acc-wpcheck.fr", ""),
				ExpectError: regexp.MustCompile(`admin_user is required to install the site`),
			},
			{
				// The API's password rule is checked at plan time.
				Config: wordPressConfig(f, id, "tf-acc-wpcheck.fr", strings.Replace(wordPressAdmins, "Correct-Horse-9",
					"correct-horse-battery", 1)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Weak password`),
			},
			{
				Config:      wordPressConfig(f, id, "tf-acc-wpcheck.fr", wordPressAdmins+"\n  path = \"/blog/\""),
				ExpectError: regexp.MustCompile(`without\s+leading\s+or\s+trailing\s+slash`),
			},
			{
				Config:      wordPressConfig(f, pending.ID(), "tf-acc-wpcheck.fr", wordPressAdmins),
				ExpectError: regexp.MustCompile(`only while its web\s+hosting is active`),
			},
			{
				// The API's own refusal reaches the user: here, a PHP version too old.
				Config:      wordPressConfig(f, id, "tf-acc-wpcheck.fr", wordPressAdmins),
				ExpectError: regexp.MustCompile(`WordPress requires PHP 8.1`),
			},
			{
				// With a PHP version resource set first, the install goes through.
				Config: acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_hosting_php_version" "test" {
  hosting_id  = %q
  php_version = "ea-php83"
}

resource "onetsolutions_hosting_wordpress" "test" {
  hosting_id          = onetsolutions_hosting_php_version.test.hosting_id
  domain              = onetsolutions_hosting_php_version.test.domain
  deletion_protection = true
%s
}
`, id, wordPressAdmins)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config:      acctest.Config(f, ""),
				ExpectError: regexp.MustCompile(`protected from deletion`),
			},
			{
				Config: acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_hosting_php_version" "test" {
  hosting_id  = %q
  php_version = "ea-php83"
}

resource "onetsolutions_hosting_wordpress" "test" {
  hosting_id = onetsolutions_hosting_php_version.test.hosting_id
  domain     = onetsolutions_hosting_php_version.test.domain
%s
}
`, id, wordPressAdmins)),
			},
		},
	})
	if sites := f.All(fakeapi.KindWordPressSite); len(sites) != 0 {
		t.Fatalf("%d sites left after the destroy", len(sites))
	}
}

// Sites gone with their web hosting are forgotten.
func TestWordPressResource_goneWithHosting(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-wpgone.fr"})
	config := wordPressConfig(f, hosting.ID(), "tf-acc-wpgone.fr", wordPressAdmins)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindWebHosting, hosting.ID(), func(o fakeapi.Object) { o["status"] = "cancelled" })
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{Config: acctest.Config(f, "")},
		},
	})
}
