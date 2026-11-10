package network_test

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

// envAccInstanceID names the existing instance of the test project the acceptance tests attach
// their firewall rules, snapshots and reverse DNS to: they never order an instance themselves.
const envAccInstanceID = "ONETSOLUTIONS_ACC_INSTANCE_ID"

func accPreCheck(t *testing.T) func() {
	t.Helper()
	return func() {
		if os.Getenv(envAccInstanceID) == "" {
			t.Fatalf("%s must be set for the network acceptance tests", envAccInstanceID)
		}
	}
}

func seedInstance(f *fakeapi.Server) fakeapi.Object {
	return f.SeedInstance(f.Scope(), nil)
}

func captureID(name string, id *string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func checkIDChanged(name string, previous *string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		if rs.Primary.ID == *previous {
			return fmt.Errorf("%s kept ID %s, want a new object", name, *previous)
		}
		return nil
	}
}

func isManagedInFake(rule fakeapi.Object) bool {
	name := rule.String("name")
	return rule["source_label"] != nil ||
		strings.HasPrefix(name, fakeapi.ManagedRulePrefix) || strings.HasPrefix(name, fakeapi.LegacyManagedRulePrefix)
}

// rulesOf returns the rules of an instance in the fake, split into customer rules (by name) and
// platform-managed ones.
func rulesOf(f *fakeapi.Server, instanceID string) (customer map[string]fakeapi.Object, managed []fakeapi.Object) {
	customer = map[string]fakeapi.Object{}
	for _, rule := range f.All(fakeapi.KindFirewallRule) {
		if rule.String("instance") != instanceID {
			continue
		}
		if isManagedInFake(rule) {
			managed = append(managed, rule)
			continue
		}
		customer[rule.String("name")] = rule
	}
	return customer, managed
}

// checkCustomerRules asserts the customer rules of the instance are exactly names.
func checkCustomerRules(f *fakeapi.Server, instanceID string, names ...string) func(*terraform.State) error {
	return func(*terraform.State) error {
		customer, _ := rulesOf(f, instanceID)
		got := make([]string, 0, len(customer))
		for name := range customer {
			got = append(got, name)
		}
		sort.Strings(got)
		want := append([]string{}, names...)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("customer rules of the instance: got %v, want %v", got, want)
		}
		return nil
	}
}

// checkManagedIntact asserts every platform-managed rule of want still exists exactly as it was
// (same fields, same modification time: never written) and that no other managed rule appeared
// or vanished.
func checkManagedIntact(f *fakeapi.Server, instanceID string, want *[]fakeapi.Object) func(*terraform.State) error {
	return func(*terraform.State) error {
		_, managed := rulesOf(f, instanceID)
		if len(managed) != len(*want) {
			return fmt.Errorf("the instance has %d platform-managed rules, want %d", len(managed), len(*want))
		}
		for _, expected := range *want {
			got, ok := f.Get(fakeapi.KindFirewallRule, expected.ID())
			if !ok {
				return fmt.Errorf("platform-managed rule %q was deleted", expected.String("name"))
			}
			if !reflect.DeepEqual(got, expected) {
				return fmt.Errorf("platform-managed rule %q was changed:\n got  %v\n want %v", expected.String("name"), got, expected)
			}
		}
		return nil
	}
}

// snapshotManaged records the managed rules of the instance as they are now.
func snapshotManaged(f *fakeapi.Server, instanceID string, into *[]fakeapi.Object) {
	_, managed := rulesOf(f, instanceID)
	*into = managed
}
