package hosting_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
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
	webHostingName   = "onetsolutions_web_hosting.test"
	orderRoute       = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/order"
	cancelRoute      = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/cancel"
	changePlanRoute  = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/change-plan"
	individualPlanID = "c2e74304-62c9-4bbe-9b9e-aaf3bddef705"
	businessPlanID   = "a3db8965-dc94-485f-b0dc-c6f2d1d87e24"
)

func webHostingConfig(f *fakeapi.Server, domain, planID, extra string) string {
	return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_web_hosting" "test" {
  domain  = %q
  plan_id = %q
%s
}
`, domain, planID, extra))
}

func captureID(name string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func checkIDUnchanged(name string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		if rs.Primary.ID != *id {
			return fmt.Errorf("%s was replaced: id %s, want %s", name, rs.Primary.ID, *id)
		}
		return nil
	}
}

// checkWebHostingsCancelled is the CheckDestroy of onetsolutions_web_hosting: a destroy cancels a
// web hosting at the end of its period, so every one the state held must still exist, cancelled.
func checkWebHostingsCancelled(f *fakeapi.Server) func(*terraform.State) error {
	return func(state *terraform.State) error {
		for _, rs := range state.RootModule().Resources {
			if rs.Type != "onetsolutions_web_hosting" {
				continue
			}
			hosting, ok := f.Get(fakeapi.KindWebHosting, rs.Primary.ID)
			if !ok {
				return fmt.Errorf("web hosting %s was removed: a destroy must only cancel it", rs.Primary.ID)
			}
			if hosting["cancellation_requested_at"] == nil {
				return fmt.Errorf("web hosting %s has no cancellation requested after the destroy", rs.Primary.ID)
			}
		}
		return nil
	}
}

func planName(t *testing.T, f *fakeapi.Server, id string) string {
	t.Helper()
	plan, ok := f.Get(fakeapi.KindHostingPlan, id)
	if !ok {
		t.Fatalf("no plan %s", id)
	}
	return plan.String("name")
}

func emptyPlanAfterApply(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(webHostingName, action)},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func TestWebHostingResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkWebHostingsCancelled(f),
		Steps: []resource.TestStep{
			{
				Config:           webHostingConfig(f, "tf-acc-site.fr", individualPlanID, ""),
				ConfigPlanChecks: emptyPlanAfterApply(plancheck.ResourceActionCreate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(webHostingName, tfjsonpath.New("status"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue(webHostingName, tfjsonpath.New("billing_cycle"), knownvalue.StringExact("monthly")),
					statecheck.ExpectKnownValue(webHostingName, tfjsonpath.New("price"), knownvalue.StringExact("2.99")),
					statecheck.ExpectKnownValue(webHostingName, tfjsonpath.New("plan_name"), knownvalue.StringExact(planName(t, f, individualPlanID))),
					statecheck.ExpectKnownValue(webHostingName, tfjsonpath.New("deletion_protection"), knownvalue.Bool(false)),
				},
				Check: captureID(webHostingName, &firstID),
			},
			{
				ResourceName:            webHostingName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ImportIDFunc(webHostingName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:      webHostingName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ImportIDFunc(webHostingName),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(webHostingName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// A new plan on a longer cycle is an upgrade order, applied in place.
				Config:           webHostingConfig(f, "tf-acc-site.fr", businessPlanID, `  billing_cycle = "annually"`),
				ConfigPlanChecks: emptyPlanAfterApply(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(webHostingName, &firstID),
					resource.TestCheckResourceAttr(webHostingName, "plan_id", businessPlanID),
					resource.TestCheckResourceAttr(webHostingName, "billing_cycle", "annually"),
					resource.TestCheckResourceAttr(webHostingName, "price", "95.90"),
				),
			},
			{
				// Inputs sent only by the destroy change in place, without any request.
				Config: webHostingConfig(f, "tf-acc-site.fr", businessPlanID, `  billing_cycle = "annually"
  cancellation_reason_code = "too_expensive"
  cancellation_reason      = "We move this site to a cheaper offer next month."`),
				ConfigPlanChecks: emptyPlanAfterApply(plancheck.ResourceActionUpdate),
				Check:            checkIDUnchanged(webHostingName, &firstID),
			},
			{
				// A new domain is a new web hosting: the old one is cancelled, not deleted.
				Config:           webHostingConfig(f, "tf-acc-other.fr", businessPlanID, `  billing_cycle = "annually"`),
				ConfigPlanChecks: emptyPlanAfterApply(plancheck.ResourceActionDestroyBeforeCreate),
				Check: func(*terraform.State) error {
					old, ok := f.Get(fakeapi.KindWebHosting, firstID)
					if !ok || old["cancellation_requested_at"] == nil {
						return fmt.Errorf("the replaced web hosting %s was not cancelled", firstID)
					}
					if old.String("cancellation_reason_code") != "too_expensive" {
						return fmt.Errorf("cancellation reason code %q, want too_expensive", old.String("cancellation_reason_code"))
					}
					return nil
				},
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, orderRoute); n != 2 {
		t.Fatalf("%d orders placed, want 2", n)
	}
}

func TestWebHostingResource_driftRecreates(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	config := webHostingConfig(f, "tf-acc-drift.fr", individualPlanID, "")
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(webHostingName, &id)},
			{
				PreConfig:        func() { f.Delete(fakeapi.KindWebHosting, id) },
				Config:           config,
				ConfigPlanChecks: emptyPlanAfterApply(plancheck.ResourceActionCreate),
			},
			{
				// Terminated outside Terraform: the next plan orders a new one.
				PreConfig: func() {
					current := f.All(fakeapi.KindWebHosting)
					f.Mutate(fakeapi.KindWebHosting, current[len(current)-1].ID(), func(o fakeapi.Object) { o["status"] = "terminated" })
				},
				Config:           config,
				ConfigPlanChecks: emptyPlanAfterApply(plancheck.ResourceActionCreate),
			},
		},
	})
}

// A cancellation requested in the console keeps the web hosting in the state, unchanged.
func TestWebHostingResource_cancelledOutsideStays(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	config := webHostingConfig(f, "tf-acc-console.fr", individualPlanID, "")
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(webHostingName, &id)},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindWebHosting, id, func(o fakeapi.Object) { o["cancellation_requested_at"] = "2026-11-01T10:00:00Z" })
				},
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr(webHostingName, "cancellation_requested_at", "2026-11-01T10:00:00Z"),
			},
		},
	})
	// The destroy found the cancellation already requested and sent nothing new.
	if hosting, _ := f.Get(fakeapi.KindWebHosting, id); hosting.String("cancellation_requested_at") != "2026-11-01T10:00:00Z" {
		t.Fatalf("cancellation_requested_at changed to %v", hosting["cancellation_requested_at"])
	}
}

// An order the balance does not cover keeps the pending web hosting in the state, so applying
// again orders nothing; it becomes active once paid.
func TestWebHostingResource_unpaidOrder(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t, fakeapi.WithUnpaidHostingOrders())
	config := webHostingConfig(f, "tf-acc-unpaid.fr", individualPlanID, "")
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkWebHostingsCancelled(f),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					captureID(webHostingName, &id),
					resource.TestCheckResourceAttr(webHostingName, "status", "pending"),
				),
				ConfigPlanChecks: emptyPlanAfterApply(plancheck.ResourceActionCreate),
			},
			{
				PreConfig:        func() { f.PayHostingOrder(id) },
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(webHostingName, &id),
					resource.TestCheckResourceAttr(webHostingName, "status", "active"),
				),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, orderRoute); n != 1 {
		t.Fatalf("%d orders placed, want 1", n)
	}
}

func TestWebHostingResource_deletionProtection(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	protected := webHostingConfig(f, "tf-acc-protected.fr", individualPlanID, "  deletion_protection = true")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkWebHostingsCancelled(f),
		Steps: []resource.TestStep{
			{Config: protected},
			{Config: protected, Destroy: true, ExpectError: regexp.MustCompile(`protected from deletion`)},
			{
				Config:      webHostingConfig(f, "tf-acc-replaced.fr", individualPlanID, "  deletion_protection = true"),
				ExpectError: regexp.MustCompile(`protected from deletion`),
			},
			{Config: webHostingConfig(f, "tf-acc-protected.fr", individualPlanID, "")},
		},
	})
	if n := f.CountRequests(http.MethodPost, orderRoute); n != 1 {
		t.Fatalf("%d orders placed, want 1", n)
	}
}

func TestWebHostingResource_planTimeChecks(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	unknownPlan := "00000000-0000-4000-8000-000000000000"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: webHostingConfig(f, "tf-acc-check.fr", unknownPlan, ""), ExpectError: regexp.MustCompile(`Plan not on sale`)},
			{
				Config:      webHostingConfig(f, "tf-acc-check.fr", individualPlanID, `  billing_cycle = "quarterly"`),
				ExpectError: regexp.MustCompile(`Billing cycle not offered`),
			},
			{
				Config:      webHostingConfig(f, "tf-acc-check.fr", individualPlanID, `  billing_cycle = "free"`),
				ExpectError: regexp.MustCompile(`Billing cycle not orderable`),
			},
			{
				Config:      webHostingConfig(f, "tf-acc-check.fr", individualPlanID, `  domain_source = "transfer"`),
				ExpectError: regexp.MustCompile(`epp_code is required for a transfer`),
			},
			{
				Config:      webHostingConfig(f, "tf-acc-check.fr", individualPlanID, `  epp_code = "secret"`),
				ExpectError: regexp.MustCompile(`epp_code is only for a transfer`),
			},
			{
				Config:      webHostingConfig(f, "Tf-Acc.fr", individualPlanID, ""),
				ExpectError: regexp.MustCompile(`lowercase domain name`),
			},
			{
				Config:      webHostingConfig(f, "tf-acc-check.fr", individualPlanID, `  cancellation_reason = "Too short."`),
				ExpectError: regexp.MustCompile(`Reason too short`),
			},
			{Config: webHostingConfig(f, "tf-acc-check.fr", individualPlanID, "")},
			{
				Config:      webHostingConfig(f, "tf-acc-check.fr", individualPlanID, `  billing_cycle = "annually"`),
				ExpectError: regexp.MustCompile(`The billing cycle cannot change alone`),
			},
			{
				Config:      webHostingConfig(f, "tf-acc-check.fr", unknownPlan, ""),
				ExpectError: regexp.MustCompile(`Plan not eligible`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, orderRoute); n != 1 {
		t.Fatalf("%d orders placed, want 1", n)
	}
	if n := f.CountRequests(http.MethodPost, changePlanRoute); n != 0 {
		t.Fatalf("%d plan changes ordered, want 0", n)
	}
}

func TestWebHostingResource_registerAndTransfer(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	f.SeedDomain(f.Scope(), fakeapi.Object{"name": "tf-acc-owned.fr"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkWebHostingsCancelled(f),
		Steps: []resource.TestStep{
			{
				Config:      webHostingConfig(f, "taken-domain.fr", individualPlanID, `  domain_source = "register"`),
				ExpectError: regexp.MustCompile(`Domain not available for registration`),
			},
			{
				Config: acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_web_hosting" "registered" {
  domain        = "tf-acc-new.fr"
  plan_id       = %q
  domain_source = "register"
}

resource "onetsolutions_web_hosting" "transferred" {
  domain        = "tf-acc-moved.fr"
  plan_id       = %q
  domain_source = "transfer"
  epp_code      = "AbC-123-xyz"
}

resource "onetsolutions_web_hosting" "existing" {
  domain        = "tf-acc-owned.fr"
  plan_id       = %q
  domain_source = "existing"
}
`, individualPlanID, individualPlanID, individualPlanID)),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("onetsolutions_web_hosting.registered", "status", "active"),
					resource.TestCheckResourceAttr("onetsolutions_web_hosting.transferred", "status", "active"),
					resource.TestCheckResourceAttr("onetsolutions_web_hosting.existing", "status", "active"),
				),
			},
			{
				// The domain source applies to the order only: a change is stored, nothing replaced.
				Config: acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_web_hosting" "registered" {
  domain        = "tf-acc-new.fr"
  plan_id       = %q
  domain_source = "external"
}
`, individualPlanID)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("onetsolutions_web_hosting.registered", plancheck.ResourceActionUpdate),
					},
				},
			},
		},
	})
}

func TestWebHostingResource_orderFailures(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	f.Inject(http.MethodPost, orderRoute, fakeapi.Injection{Status: http.StatusBadRequest,
		Body: fakeapi.Object{"error": "Failed to create hosting order. Please try again later."}})
	f.Inject(http.MethodPost, orderRoute, fakeapi.Injection{Status: http.StatusBadGateway, AfterHandling: true})
	config := webHostingConfig(f, "tf-acc-failure.fr", individualPlanID, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, ExpectError: regexp.MustCompile(`Could not order the web hosting`)},
			{Config: config, ExpectError: regexp.MustCompile(`The web hosting may have been ordered`)},
		},
	})
	// The lost answer placed an order the provider never retried.
	if n := f.CountRequests(http.MethodPost, orderRoute); n != 2 {
		t.Fatalf("%d order requests, want 2", n)
	}
}

// A web hosting not linked to its billing service yet cannot be cancelled: the destroy retries.
func TestWebHostingResource_deleteRetriesUntilLinked(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if n := f.CountRequests(http.MethodPost, cancelRoute); n != 3 {
				return fmt.Errorf("cancel sent %d times, want 3", n)
			}
			return checkWebHostingsCancelled(f)(s)
		},
		Steps: []resource.TestStep{
			{
				Config: webHostingConfig(f, "tf-acc-linking.fr", individualPlanID, ""),
				Check: func(*terraform.State) error {
					f.Inject(http.MethodPost, cancelRoute, fakeapi.Injection{Status: http.StatusBadRequest, Times: 2,
						Body: fakeapi.Object{"detail": "Operation failed. Please try again later.", "code": "hosting_not_linked_to_billing"}})
					return nil
				},
			},
		},
	})
}

// A plan change whose invoice stays unpaid times out with the invoice, and the state keeps the
// plan the web hosting still has.
func TestWebHostingResource_unpaidPlanChange(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t, fakeapi.WithUnpaidHostingPlanChanges())
	timeouts := `  timeouts {
    update = "2s"
  }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: webHostingConfig(f, "tf-acc-upgrade.fr", individualPlanID, timeouts)},
			{
				Config:      webHostingConfig(f, "tf-acc-upgrade.fr", businessPlanID, timeouts),
				ExpectError: regexp.MustCompile(`invoices/920001`),
			},
			{
				Config:           webHostingConfig(f, "tf-acc-upgrade.fr", individualPlanID, timeouts),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr(webHostingName, "plan_id", individualPlanID),
			},
			{
				// The first change still awaits payment: a second one is refused.
				Config:      webHostingConfig(f, "tf-acc-upgrade.fr", businessPlanID, timeouts),
				ExpectError: regexp.MustCompile(`awaiting payment`),
			},
		},
	})
}

func TestWebHostingResource_importPlansNothing(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	seeded := f.SeedWebHosting(f.Scope(), fakeapi.Object{"domain": "tf-acc-imported.fr", "billing_cycle": "annually"})
	config := webHostingConfig(f, "tf-acc-imported.fr", seeded.Map("plan").ID(), "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:          config,
				ResourceName:    webHostingName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   seeded.ID(),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(webHostingName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config:             config,
				ResourceName:       webHostingName,
				ImportState:        true,
				ImportStateId:      seeded.ID(),
				ImportStatePersist: true,
			},
			{
				// The cycle of an imported web hosting is kept when the configuration leaves it unset.
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr(webHostingName, "billing_cycle", "annually"),
			},
			{
				// An order input set after an import is stored without a replacement.
				Config:           strings.Replace(config, "plan_id", "domain_source = \"existing\"\n  plan_id", 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(webHostingName, plancheck.ResourceActionUpdate)}},
				Check:            resource.TestCheckResourceAttr(webHostingName, "id", seeded.ID()),
			},
		},
	})
}
