package dns_test

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
	domainName       = "onetsolutions_domain.test"
	domainOrderRoute = "/v1/organizations/{organization_id}/projects/{project_id}/domains/order"
)

func domainConfig(f *fakeapi.Server, name, extra string) string {
	return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_domain" "test" {
  name = %q
%s
}`, name, extra))
}

// domainsKept is the CheckDestroy of onetsolutions_domain: a destroy never deletes a domain.
func domainsKept(f *fakeapi.Server) func(*terraform.State) error {
	return func(state *terraform.State) error {
		for _, rs := range state.RootModule().Resources {
			if rs.Type != "onetsolutions_domain" {
				continue
			}
			domain, ok := f.Get(fakeapi.KindDomain, rs.Primary.ID)
			if !ok || domain["status"] == "deleted" {
				return fmt.Errorf("domain %s was deleted by the destroy", rs.Primary.ID)
			}
		}
		return nil
	}
}

func domainByName(t *testing.T, f *fakeapi.Server, name string) fakeapi.Object {
	t.Helper()
	for _, domain := range f.All(fakeapi.KindDomain) {
		if domain.String("name") == name {
			return domain
		}
	}
	t.Fatalf("no domain %s in the fake", name)
	return nil
}

func countOrders(f *fakeapi.Server, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if n := f.CountRequests(http.MethodPost, domainOrderRoute); n != want {
			return fmt.Errorf("%d domain orders, want %d", n, want)
		}
		return nil
	}
}

func TestDomainResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             domainsKept(f),
		Steps: []resource.TestStep{
			{
				Config: domainConfig(f, "acme-shop.com", ""),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("status"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("deletion_protection"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("auto_renew"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("recurring_amount"), knownvalue.StringExact("13.99")),
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("expiration_date"), knownvalue.StringRegexp(regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`))),
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("dns_zone_id"), knownvalue.Null()),
				},
				Check:            resource.ComposeTestCheckFunc(captureID(domainName, &firstID), countOrders(f, 1)),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				ResourceName:      domainName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ImportIDFunc(domainName),
				ImportStateVerify: true,
			},
			{
				ResourceName:    domainName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				Config:          domainConfig(f, "acme-shop.com", ""),
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return firstID, nil
				},
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(domainName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// deletion_protection changes in place, without an order.
				Config: domainConfig(f, "acme-shop.com", "  deletion_protection = true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(domainName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(domainName, "deletion_protection", "true"), countOrders(f, 1)),
			},
			{
				// Protected: the replacement is refused before anything is ordered.
				Config:      domainConfig(f, "acme-store.com", "  deletion_protection = true"),
				ExpectError: regexp.MustCompile(`The domain is protected from deletion`),
			},
			{
				Config: domainConfig(f, "acme-shop.com", ""),
				Check:  countOrders(f, 1),
			},
			{
				// A new name registers a new domain; the old one stays registered.
				Config: domainConfig(f, "acme-store.com", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(domainName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					countOrders(f, 2),
					func(*terraform.State) error {
						old, ok := f.Get(fakeapi.KindDomain, firstID)
						if !ok || old["status"] != "active" {
							return fmt.Errorf("the replaced domain is %v, want it still active", old)
						}
						return nil
					},
				),
			},
			{
				// Deleted outside Terraform: the next plan orders it again.
				PreConfig: func() {
					current := domainByName(t, f, "acme-store.com")
					f.Delete(fakeapi.KindDomain, current.ID())
				},
				Config:             domainConfig(f, "acme-store.com", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config:           domainConfig(f, "acme-store.com", ""),
				Check:            countOrders(f, 3),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
		},
	})
}

func TestDomainResource_refusedAtPlanTime(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	f.SeedDomain(f.Scope(), fakeapi.Object{"name": "mine.fr"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      domainConfig(f, "taken-shop.com", ""),
				ExpectError: regexp.MustCompile(`(?s)Domain not available.*taken-shop.fr \(7.99 EUR\)`),
			},
			{
				Config:      domainConfig(f, "mine.fr", ""),
				ExpectError: regexp.MustCompile(`(?s)The domain is already in the project.*terraform import`),
			},
			{
				Config:      domainConfig(f, "startup.io", ""),
				ExpectError: regexp.MustCompile(`Extension not on sale`),
			},
			{
				Config:      domainConfig(f, "Shop.com", ""),
				ExpectError: regexp.MustCompile(`must be lowercase`),
			},
			{
				Config:      domainConfig(f, "localhost", ""),
				ExpectError: regexp.MustCompile(`is not a domain name such as example\.com`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, domainOrderRoute); n != 0 {
		t.Fatalf("%d orders placed, want 0", n)
	}
}

func TestDomainResource_unpaidOrder(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t, fakeapi.WithUnpaidDomainOrders(), fakeapi.WithTaskPolls(1))
	config := domainConfig(f, "unpaid.fr", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             domainsKept(f),
		Steps: []resource.TestStep{
			{
				// The order awaits payment: the domain is tracked, pending, with no error.
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("status"), knownvalue.StringExact("pending")),
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("expiration_date"), knownvalue.Null()),
				},
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				// Paid in the console: the refresh reads the registration, and nothing is planned.
				PreConfig: func() {
					domain := domainByName(t, f, "unpaid.fr")
					f.PayDomainOrder(domain.ID())
				},
				Config:           config,
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(domainName, "status", "active"),
					resource.TestCheckResourceAttrSet(domainName, "expiration_date"),
					countOrders(f, 1),
				),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
		},
	})
}

func TestDomainResource_orderFailures(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	f.Inject(http.MethodPost, domainOrderRoute, fakeapi.Injection{Status: http.StatusBadGateway, AfterHandling: true})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The answer is lost after the order was placed: nothing in the state, no retry.
				Config:      domainConfig(f, "lost.com", ""),
				ExpectError: regexp.MustCompile(`The domain may have been ordered`),
				Check:       countOrders(f, 1),
			},
			{
				// The next plan finds it in the project instead of ordering it again.
				Config:      domainConfig(f, "lost.com", ""),
				ExpectError: regexp.MustCompile(`The domain is already in the project`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, domainOrderRoute); n != 1 {
		t.Fatalf("%d orders placed, want 1", n)
	}
}

func TestDomainResource_registrationFailed(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	f.FailNextDomainRegistration()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             domainsKept(f),
		Steps: []resource.TestStep{
			{
				Config:      domainConfig(f, "doomed.com", ""),
				ExpectError: regexp.MustCompile(`The registration of the domain failed`),
			},
			{
				// Tainted: the next apply orders it again, and it is registered.
				Config: domainConfig(f, "doomed.com", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(domainName, plancheck.ResourceActionReplace)},
				},
				Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr(domainName, "status", "active"), countOrders(f, 2)),
			},
		},
	})
}

func TestDomainResource_registrationSlowerThanTimeout(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t, fakeapi.WithTaskPolls(100000))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             domainsKept(f),
		Steps: []resource.TestStep{
			{
				// A timeout only warns: the domain is tracked, not tainted, and never ordered twice. The
				// timeout leaves room for the order request itself under a loaded -race run, so it is the
				// wait for the never-ending task that runs out.
				Config: domainConfig(f, "slow.com", `  timeouts {
    create = "2s"
  }`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(domainName, tfjsonpath.New("status"), knownvalue.StringExact("pending")),
				},
				ConfigPlanChecks: emptyPlanAfterApply(),
				Check:            countOrders(f, 1),
			},
		},
	})
}

func TestDomainAvailabilityDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, `data "onetsolutions_domain_availability" "x" { name = "bad_name.com" }`),
				ExpectError: regexp.MustCompile(`Invalid domain name`),
			},
			{
				Config: acctest.Config(f, `
data "onetsolutions_domain_availability" "free" {
  name = "fresh-idea.fr"
}
data "onetsolutions_domain_availability" "taken" {
  name = "taken-brand.com"
}`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_domain_availability.free", tfjsonpath.New("available"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("data.onetsolutions_domain_availability.free", tfjsonpath.New("tld"), knownvalue.StringExact("fr")),
					statecheck.ExpectKnownValue("data.onetsolutions_domain_availability.free", tfjsonpath.New("price"), knownvalue.StringExact("7.99")),
					statecheck.ExpectKnownValue("data.onetsolutions_domain_availability.free", tfjsonpath.New("suggestions"), knownvalue.ListSizeExact(0)),
					statecheck.ExpectKnownValue("data.onetsolutions_domain_availability.taken", tfjsonpath.New("available"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue("data.onetsolutions_domain_availability.taken", tfjsonpath.New("price"), knownvalue.Null()),
					statecheck.ExpectKnownValue("data.onetsolutions_domain_availability.taken", tfjsonpath.New("suggestions").AtSliceIndex(0).AtMapKey("name"),
						knownvalue.StringExact("taken-brand.fr")),
				},
			},
		},
	})
}

func TestTLDsDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, `data "onetsolutions_tlds" "x" { category = "premium" }`),
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
			{
				Config: acctest.Config(f, `
data "onetsolutions_tlds" "all" {}
data "onetsolutions_tlds" "fr" {
  tld = "fr"
}
data "onetsolutions_tlds" "closed" {
  registration_available = false
}
data "onetsolutions_tlds" "country" {
  category = "country"
}`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_tlds.all", tfjsonpath.New("tlds"), knownvalue.ListSizeExact(3)),
					statecheck.ExpectKnownValue("data.onetsolutions_tlds.fr", tfjsonpath.New("tlds"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"tld":                    knownvalue.StringExact("fr"),
							"category":               knownvalue.StringExact("country"),
							"registration_available": knownvalue.Bool(true),
							"pricing": knownvalue.ListExact([]knownvalue.Check{knownvalue.ObjectExact(map[string]knownvalue.Check{
								"years":              knownvalue.Int64Exact(1),
								"registration_price": knownvalue.StringExact("7.99"),
								"renewal_price":      knownvalue.StringExact("9.99"),
								"transfer_price":     knownvalue.StringExact("7.99"),
							})}),
						}),
					})),
					statecheck.ExpectKnownValue("data.onetsolutions_tlds.closed", tfjsonpath.New("tlds").AtSliceIndex(0).AtMapKey("tld"), knownvalue.StringExact("io")),
					statecheck.ExpectKnownValue("data.onetsolutions_tlds.country", tfjsonpath.New("tlds"), knownvalue.ListSizeExact(1)),
				},
			},
		},
	})
}
