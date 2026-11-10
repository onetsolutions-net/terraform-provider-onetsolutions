package network_test

import (
	"fmt"
	"net/http"
	"os"
	"reflect"
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
	firewallName       = "onetsolutions_instance_firewall.test"
	firewallRulesRoute = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/firewall"
	firewallApplyRoute = firewallRulesRoute + "/apply"
)

func firewallConfig(instanceID, rules string) string {
	return fmt.Sprintf(`
resource "onetsolutions_instance_firewall" "test" {
  instance_id = %q
%s
}
`, instanceID, rules)
}

const (
	sshRule = `
  rule {
    name             = "ssh"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    source_cidr      = "198.51.100.0/24"
    destination_port = "22"
    priority         = 100
  }`
	httpsRule = `
  rule {
    name             = "https"
    description      = "Public web"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    destination_port = "443,8443"
    destination_cidr = "203.0.113.0/24"
    priority         = 200
  }`
	icmpRule = `
  rule {
    name      = "ping"
    direction = "inbound"
    action    = "drop"
    protocol  = "icmp"
    priority  = 300
    enabled   = false
  }`
	sshRuleChanged = `
  rule {
    name             = "ssh"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    destination_port = "2222"
    priority         = 110
  }`
	dnsRule = `
  rule {
    name             = "dns-out"
    direction        = "outbound"
    action           = "accept"
    protocol         = "udp"
    destination_port = "53"
    priority         = 400
  }`
)

func TestInstanceFirewallResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	var managed []fakeapi.Object
	f.SeedPlatformFirewallRules(f.Scope(), instanceID)
	snapshotManaged(f, instanceID, &managed)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeTestCheckFunc(
			checkCustomerRules(f, instanceID),
			checkManagedIntact(f, instanceID, &managed),
		),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, firewallConfig(instanceID, sshRule+httpsRule+icmpRule)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(firewallName, tfjsonpath.New("id"), knownvalue.StringExact(instanceID)),
					statecheck.ExpectKnownValue(firewallName, tfjsonpath.New("rule"), knownvalue.ListSizeExact(3)),
					statecheck.ExpectKnownValue(firewallName, tfjsonpath.New("rule").AtSliceIndex(0).AtMapKey("description"), knownvalue.StringExact("")),
					statecheck.ExpectKnownValue(firewallName, tfjsonpath.New("rule").AtSliceIndex(0).AtMapKey("enabled"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(firewallName, tfjsonpath.New("rule").AtSliceIndex(1).AtMapKey("source_cidr"), knownvalue.StringExact("0.0.0.0/0")),
					statecheck.ExpectKnownValue(firewallName, tfjsonpath.New("rule").AtSliceIndex(2).AtMapKey("destination_port"), knownvalue.Null()),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkCustomerRules(f, instanceID, "ssh", "https", "ping"),
					checkManagedIntact(f, instanceID, &managed),
					checkRule(f, instanceID, "https", fakeapi.Object{
						"destination_cidr": "203.0.113.0/24", "destination_port": "443,8443", "description": "Public web",
					}),
					checkRule(f, instanceID, "ping", fakeapi.Object{"is_enabled": false, "destination_port": ""}),
					// Only what the create cannot set is patched (https's destination_cidr, ping
					// disabled), and the apply queued by the last change is the one waited for.
					checkPatchedRules(f, instanceID, "https", "ping"),
					checkRequests(f, http.MethodPost, firewallApplyRoute, 0),
					checkFirewallApplied(f, instanceID),
				),
			},
			{
				ResourceName:            firewallName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ImportIDFunc(firewallName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:      firewallName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ImportIDFunc(firewallName),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(firewallName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// Change one rule, drop one, rename none, add one: one apply of the whole set.
				Config: acctest.Config(f, firewallConfig(instanceID, sshRuleChanged+icmpRule+dnsRule)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(firewallName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkCustomerRules(f, instanceID, "ssh", "ping", "dns-out"),
					checkManagedIntact(f, instanceID, &managed),
					checkRule(f, instanceID, "ssh", fakeapi.Object{
						"destination_port": "2222", "priority": float64(110), "source_cidr": "0.0.0.0/0",
					}),
				),
			},
			{
				// Rules removed and added outside Terraform are drift; platform rules changed or
				// added outside Terraform are not.
				PreConfig: func() {
					customer, _ := rulesOf(f, instanceID)
					f.Delete(fakeapi.KindFirewallRule, customer["ping"].ID())
					f.SeedFirewallRule(f.Scope(), instanceID, fakeapi.Object{"name": "added-in-console", "priority": 50})
					f.Mutate(fakeapi.KindFirewallRule, managed[1].ID(), func(o fakeapi.Object) { o["destination_port"] = "4443" })
					f.SeedFirewallRule(f.Scope(), instanceID, fakeapi.Object{
						"name": fakeapi.ManagedRulePrefix + "Realtime (6001)", "destination_port": "6001", "priority": 25,
					})
					snapshotManaged(f, instanceID, &managed)
				},
				Config: acctest.Config(f, firewallConfig(instanceID, sshRuleChanged+icmpRule+dnsRule)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(firewallName, plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkCustomerRules(f, instanceID, "ssh", "ping", "dns-out"),
					checkManagedIntact(f, instanceID, &managed),
				),
			},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindFirewallRule, managed[0].ID(), func(o fakeapi.Object) { o["is_enabled"] = false })
					f.SeedFirewallRule(f.Scope(), instanceID, fakeapi.Object{
						"name": "office", "source_cidr": "192.0.2.10/32", "source_label": fakeapi.ControlPlaneLabel, "priority": 5,
					})
					snapshotManaged(f, instanceID, &managed)
				},
				Config: acctest.Config(f, firewallConfig(instanceID, sshRuleChanged+icmpRule+dnsRule)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// No rule block at all: every customer rule goes, the platform rules stay.
				Config: acctest.Config(f, firewallConfig(instanceID, "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkCustomerRules(f, instanceID),
					checkManagedIntact(f, instanceID, &managed),
				),
			},
			{
				Config: acctest.Config(f, firewallConfig(instanceID, sshRule)),
				Check:  checkCustomerRules(f, instanceID, "ssh"),
			},
		},
	})
}

func checkRule(f *fakeapi.Server, instanceID, name string, want fakeapi.Object) func(*terraform.State) error {
	return func(*terraform.State) error {
		customer, _ := rulesOf(f, instanceID)
		rule, ok := customer[name]
		if !ok {
			return fmt.Errorf("rule %q not found", name)
		}
		for key, value := range want {
			if rule[key] != value {
				return fmt.Errorf("rule %q: %s = %v, want %v", name, key, rule[key], value)
			}
		}
		return nil
	}
}

func checkRequests(f *fakeapi.Server, method, route string, want int) func(*terraform.State) error {
	return func(*terraform.State) error {
		if got := f.CountRequests(method, route); got != want {
			return fmt.Errorf("%s %s sent %d times, want %d", method, route, got, want)
		}
		return nil
	}
}

// checkPatchedRules asserts the rules a PATCH was sent to (refused attempts included) are exactly
// names.
func checkPatchedRules(f *fakeapi.Server, instanceID string, names ...string) func(*terraform.State) error {
	return func(*terraform.State) error {
		customer, _ := rulesOf(f, instanceID)
		byPath := map[string]string{}
		for name, rule := range customer {
			byPath[fmt.Sprintf("/v1/organizations/%s/projects/%s/compute/instances/%s/firewall/%s",
				f.Scope().OrganizationID, f.Scope().ProjectID, instanceID, rule.ID())] = name
		}
		patched := map[string]bool{}
		for _, r := range f.Requests() {
			if r.Method == http.MethodPatch && r.Template == firewallRulesRoute+"/{id}" {
				patched[byPath[r.Path]] = true
			}
		}
		want := map[string]bool{}
		for _, name := range names {
			want[name] = true
		}
		if !reflect.DeepEqual(patched, want) {
			return fmt.Errorf("rules patched: %v, want %v", patched, want)
		}
		return nil
	}
}

// checkFirewallApplied asserts the firewall was applied and the provider waited for it: every
// apply_firewall task of the instance has completed.
func checkFirewallApplied(f *fakeapi.Server, instanceID string) func(*terraform.State) error {
	return func(*terraform.State) error {
		applies := 0
		for _, task := range f.All(fakeapi.KindInstanceTask) {
			if task.String("instance_id") != instanceID || task.String("task_type") != fakeapi.TaskApplyFirewall {
				continue
			}
			applies++
			if task.String("status") != "completed" {
				return fmt.Errorf("apply_firewall %s is %s: the provider did not wait for it", task.ID(), task.String("status"))
			}
		}
		if applies == 0 {
			return fmt.Errorf("the firewall was never applied")
		}
		return nil
	}
}

// Every rule change queues a synchronization, so the next change meets a 409 until it ends: the
// provider retries. An apply of its own (no rule changed) is retried the same way.
func TestInstanceFirewallResource_retriesWhileBusy(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             checkCustomerRules(f, instanceID),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, firewallConfig(instanceID, sshRule+httpsRule)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkCustomerRules(f, instanceID, "ssh", "https"),
					checkFirewallApplied(f, instanceID),
					func(*terraform.State) error {
						if n := f.CountRequests(http.MethodPost, firewallRulesRoute); n <= 2 {
							return fmt.Errorf("POST rule sent %d times: the second create should have met a 409 and been retried", n)
						}
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					f.Inject(http.MethodPost, firewallApplyRoute, fakeapi.Injection{
						Status: http.StatusConflict,
						Body: fakeapi.Object{
							"detail": "A task is already in progress on this instance. Please try again once it has finished.",
							"code":   "instance_task_in_progress",
						},
						Times: 3,
					})
				},
				Config: acctest.Config(f, firewallConfig(instanceID, sshRule+httpsRule+`
  timeouts {
    update = "5m"
  }`)),
				Check: resource.ComposeTestCheckFunc(
					checkFirewallApplied(f, instanceID),
					checkRequests(f, http.MethodPost, firewallApplyRoute, 4),
				),
			},
		},
	})
}

func TestInstanceFirewallResource_applyFailure(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	f.FailNextTask("apply_firewall", "the hypervisor refused the rule set")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, "")),
				ExpectError: regexp.MustCompile(`the\s+hypervisor\s+refused\s+the\s+rule\s+set`),
			},
			{
				// Nothing was recorded, and creating again adopts the rules by name.
				Config: acctest.Config(f, firewallConfig(instanceID, sshRule)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(firewallName, plancheck.ResourceActionCreate)},
				},
				Check: checkCustomerRules(f, instanceID, "ssh"),
			},
		},
	})
}

// Rules that already exist with a configured name are adopted, not duplicated.
func TestInstanceFirewallResource_adoptsExistingRulesByName(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	existing := f.SeedFirewallRule(f.Scope(), instanceID, fakeapi.Object{"name": "ssh", "destination_port": "2200", "priority": 7})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, firewallConfig(instanceID, sshRule)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkCustomerRules(f, instanceID, "ssh"),
					func(*terraform.State) error {
						rule, ok := f.Get(fakeapi.KindFirewallRule, existing.ID())
						if !ok {
							return fmt.Errorf("the existing rule was deleted instead of updated")
						}
						if rule["destination_port"] != "22" || rule["source_cidr"] != "198.51.100.0/24" {
							return fmt.Errorf("the existing rule was not updated: %v", rule)
						}
						return nil
					},
				),
			},
		},
	})
}

// A rule from the control plane's egress address is platform-managed: the provider refuses it on
// create and on update, so no customer rule turns into one Terraform can no longer see or delete.
func TestInstanceFirewallResource_platformSourceRefused(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	apiRule := func(source string) string {
		return fmt.Sprintf(`
  rule {
    name             = "api"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    source_cidr      = %q
    destination_port = "8080"
    priority         = 100
  }`, source)
	}
	ruleSources := func(want ...string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			var got []string
			for _, rule := range f.All(fakeapi.KindFirewallRule) {
				if rule.String("name") == "api" {
					got = append(got, rule.String("source_cidr"))
				}
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				return fmt.Errorf("rules named api have sources %v, want %v", got, want)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             func(s *terraform.State) error { return ruleSources()(s) },
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, apiRule(fakeapi.ControlPlaneEgressCIDR))),
				ExpectError: regexp.MustCompile(`platform address`),
			},
			{
				Config: acctest.Config(f, firewallConfig(instanceID, apiRule("203.0.113.0/24"))),
				Check:  ruleSources("203.0.113.0/24"),
			},
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, apiRule(fakeapi.ControlPlaneEgressCIDR))),
				ExpectError: regexp.MustCompile(`(?s)platform address.*keeps its source 203\.0\.113\.0/24`),
			},
			{
				// The rule is still a customer rule, in the state, so destroy deletes it.
				Config:           acctest.Config(f, firewallConfig(instanceID, apiRule("203.0.113.0/24"))),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            ruleSources("203.0.113.0/24"),
			},
		},
	})
}

func TestInstanceFirewallResource_instanceDeletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	config := acctest.Config(f, firewallConfig(instanceID, sshRule))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { f.Delete(fakeapi.KindInstance, instanceID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestInstanceFirewallResource_invalidAtPlan(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	rule := func(name, protocol, port string) string {
		return fmt.Sprintf(`
  rule {
    name             = %q
    direction        = "inbound"
    action           = "accept"
    protocol         = %q
    destination_port = %s
    priority         = 100
  }`, name, protocol, port)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, rule("web", "tcp", `"80"`)+rule("web", "tcp", `"443"`))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Duplicate rule name`),
			},
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, rule("One Click Apps · mine", "tcp", `"80"`))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Reserved rule name`),
			},
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, rule("one-click:mine", "tcp", `"80"`))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Reserved rule name`),
			},
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, rule("ping", "icmp", `"80"`))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Port without TCP or UDP`),
			},
			{
				Config:      acctest.Config(f, firewallConfig(instanceID, rule("web", "tcp", `"0-80"`))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not\s+a\s+port\s+between\s+1\s+and\s+65535`),
			},
			{
				// The API trims names and descriptions: surrounding whitespace would never match.
				Config:      acctest.Config(f, firewallConfig(instanceID, rule("web ", "tcp", `"80"`))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Surrounding whitespace`),
			},
			{
				Config: acctest.Config(f, firewallConfig(instanceID, `
  rule {
    name        = "web"
    description = "allow web\n"
    direction   = "inbound"
    action      = "accept"
    protocol    = "tcp"
    priority    = 100
  }`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Surrounding whitespace`),
			},
		},
	})
}

func TestAccInstanceFirewall_basic(t *testing.T) {
	t.Parallel()
	instanceID := os.Getenv(envAccInstanceID)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t); accPreCheck(t)() },
		ProtoV6ProviderFactories: acctest.RealProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: firewallConfig(instanceID, `
  rule {
    name             = "tf-acc-ssh"
    direction        = "inbound"
    action           = "accept"
    protocol         = "tcp"
    destination_port = "22"
    priority         = 9000
  }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:            firewallName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ImportIDFunc(firewallName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}
