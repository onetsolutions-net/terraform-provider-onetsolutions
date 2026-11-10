package network_test

import (
	"fmt"
	"net/http"
	"os"
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
	reverseDNSName  = "onetsolutions_instance_ip_reverse_dns.test"
	ipAddressRoute  = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/ip-addresses/{id}"
	envAccIPAddress = "ONETSOLUTIONS_ACC_IP_ADDRESS"
)

func reverseDNSConfig(instanceID, selector, hostname string) string {
	return fmt.Sprintf(`
resource "onetsolutions_instance_ip_reverse_dns" "test" {
  instance_id = %q
  %s
  reverse_dns = %q
}
`, instanceID, selector, hostname)
}

// primaryAddress returns the ID and the value of the instance's primary IPv4 address.
func primaryAddress(f *fakeapi.Server, instance fakeapi.Object) (id, address string) {
	address = instance.String("primary_ipv4")
	for _, candidate := range f.All(fakeapi.KindIPAddress) {
		if candidate.String("address") == address {
			return candidate.ID(), address
		}
	}
	return "", address
}

func checkFakeReverseDNS(f *fakeapi.Server, addressID, want string) func(*terraform.State) error {
	return func(*terraform.State) error {
		address, ok := f.Get(fakeapi.KindIPAddress, addressID)
		if !ok {
			return fmt.Errorf("address %s not found", addressID)
		}
		if got := address.String("reverse_dns"); got != want {
			return fmt.Errorf("reverse DNS in the API is %q, want %q", got, want)
		}
		return nil
	}
}

func TestInstanceIPReverseDNSResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instance := seedInstance(f)
	instanceID := instance.ID()
	addressID, address := primaryAddress(f, instance)
	byAddress := fmt.Sprintf("address = %q", address)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		// Destroying only forgets the resource: the API cannot reset a reverse DNS.
		CheckDestroy: checkFakeReverseDNS(f, addressID, "mail.example.com"),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, reverseDNSConfig(instanceID, byAddress, "web.example.com")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(reverseDNSName, tfjsonpath.New("ip_address_id"), knownvalue.StringExact(addressID)),
					statecheck.ExpectKnownValue(reverseDNSName, tfjsonpath.New("id"), knownvalue.StringExact(addressID)),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkFakeReverseDNS(f, addressID, "web.example.com"),
			},
			{
				ResourceName:            reverseDNSName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ChildImportIDFunc(reverseDNSName, "instance_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:      reverseDNSName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(reverseDNSName, "instance_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(reverseDNSName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config: acctest.Config(f, reverseDNSConfig(instanceID, byAddress, "mail.example.com")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(reverseDNSName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkFakeReverseDNS(f, addressID, "mail.example.com"),
			},
			{
				// Designating the same address by its ID instead changes nothing.
				Config: acctest.Config(f, reverseDNSConfig(instanceID, fmt.Sprintf("ip_address_id = %q", addressID), "mail.example.com")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindIPAddress, addressID, func(o fakeapi.Object) { o["reverse_dns"] = "changed.example.org" })
				},
				Config: acctest.Config(f, reverseDNSConfig(instanceID, byAddress, "mail.example.com")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(reverseDNSName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkFakeReverseDNS(f, addressID, "mail.example.com"),
			},
		},
	})
}

func TestInstanceIPReverseDNSResource_otherAddressReplaces(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instance := seedInstance(f)
	instanceID := instance.ID()
	_, primary := primaryAddress(f, instance)
	secondary := f.SeedIPAddress(f.Scope(), instanceID, fakeapi.Object{"address": "198.51.100.77"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: acctest.Config(f, reverseDNSConfig(instanceID, fmt.Sprintf("address = %q", primary), "mail.example.com"))},
			{
				Config: acctest.Config(f, reverseDNSConfig(instanceID, fmt.Sprintf("address = %q", "198.51.100.77"), "mail.example.com")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(reverseDNSName, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: checkFakeReverseDNS(f, secondary.ID(), "mail.example.com"),
			},
		},
	})
}

func TestInstanceIPReverseDNSResource_deletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instance := seedInstance(f)
	addressID, address := primaryAddress(f, instance)
	config := acctest.Config(f, reverseDNSConfig(instance.ID(), fmt.Sprintf("address = %q", address), "mail.example.com"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { f.Delete(fakeapi.KindIPAddress, addressID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestInstanceIPReverseDNSResource_retriesWhileBusy(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instance := seedInstance(f)
	addressID, address := primaryAddress(f, instance)
	f.SeedInstanceTask(f.Scope(), instance.ID(), "reboot")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, reverseDNSConfig(instance.ID(), fmt.Sprintf("address = %q", address), "mail.example.com")),
				Check: resource.ComposeTestCheckFunc(
					checkFakeReverseDNS(f, addressID, "mail.example.com"),
					func(*terraform.State) error {
						if n := f.CountRequests(http.MethodPatch, ipAddressRoute); n < 2 {
							return fmt.Errorf("PATCH sent %d times, want a retry after the 409", n)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestInstanceIPReverseDNSResource_errors(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instance := seedInstance(f)
	instanceID := instance.ID()
	addressID, address := primaryAddress(f, instance)
	ipv6 := f.SeedIPAddress(f.Scope(), instanceID, fakeapi.Object{"address": "2001:db8::10", "ip_type": "ipv6"})
	f.FailNextTask("update_reverse_dns", "the hostname does not resolve to the address")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, reverseDNSConfig(instanceID, fmt.Sprintf("address = %q", address), "Mail.Example.com.")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`lowercase\s+fully\s+qualified\s+hostname`),
			},
			{
				Config:      acctest.Config(f, reverseDNSConfig(instanceID, `address = "2001:db8::10"`, "mail.example.com")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid IPv4 address`),
			},
			{
				Config: acctest.Config(f, reverseDNSConfig(instanceID,
					fmt.Sprintf("address = %q\n  ip_address_id = %q", address, addressID), "mail.example.com")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config:      acctest.Config(f, reverseDNSConfig(instanceID, `address = "192.0.2.99"`, "mail.example.com")),
				ExpectError: regexp.MustCompile(`has\s+no\s+address\s+192\.0\.2\.99`),
			},
			{
				Config:      acctest.Config(f, reverseDNSConfig(instanceID, fmt.Sprintf("ip_address_id = %q", ipv6.ID()), "mail.example.com")),
				ExpectError: regexp.MustCompile(`only\s+be\s+set\s+on\s+IPv4\s+addresses`),
			},
			{
				Config:      acctest.Config(f, reverseDNSConfig(instanceID, fmt.Sprintf("address = %q", address), "mail.example.com")),
				ExpectError: regexp.MustCompile(`the\s+hostname\s+does\s+not\s+resolve\s+to\s+the\s+address`),
			},
		},
	})
}

func TestAccInstanceIPReverseDNS_basic(t *testing.T) {
	t.Parallel()
	instanceID, address := os.Getenv(envAccInstanceID), os.Getenv(envAccIPAddress)
	hostname := os.Getenv("ONETSOLUTIONS_ACC_REVERSE_DNS")
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			accPreCheck(t)()
			if address == "" || hostname == "" {
				t.Fatalf("%s and ONETSOLUTIONS_ACC_REVERSE_DNS (a hostname resolving to it) must be set", envAccIPAddress)
			}
		},
		ProtoV6ProviderFactories: acctest.RealProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: reverseDNSConfig(instanceID, fmt.Sprintf("address = %q", address), hostname),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:            reverseDNSName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ChildImportIDFunc(reverseDNSName, "instance_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}
