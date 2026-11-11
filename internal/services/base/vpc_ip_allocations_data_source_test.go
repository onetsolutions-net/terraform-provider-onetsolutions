package base_test

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

func TestVPCIPAllocationsDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	vpc := f.SeedVPC(scope, fakeapi.Object{"name": "backend"}).ID()
	empty := f.SeedVPC(scope, fakeapi.Object{"name": "empty", "cidr_block": "10.20.0.0/16"}).ID()
	instance := f.SeedInstance(scope, fakeapi.Object{"name": "web-1", "vpc_id": vpc})
	f.SeedVPCIPAllocation(scope, vpc, fakeapi.Object{"ip_address": "10.10.0.5", "instance_id": instance.ID()})
	f.SeedVPCIPAllocation(scope, vpc, nil)
	f.SeedVPCIPAllocation(scope, vpc, fakeapi.Object{"ip_address": "10.10.0.9", "status": "released"})
	const name = "data.onetsolutions_vpc_ip_allocations.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_vpc_ip_allocations" "test" {
  vpc_id = %q
}
data "onetsolutions_vpc_ip_allocations" "empty" {
  vpc_id = %q
}
`, vpc, empty)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(name, tfjsonpath.New("allocations"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"ip_address":      knownvalue.StringExact("10.10.0.1"),
							"allocation_type": knownvalue.StringExact("gateway"),
							"instance_id":     knownvalue.Null(),
							"instance_name":   knownvalue.Null(),
						}),
						knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"ip_address":      knownvalue.StringExact("10.10.0.5"),
							"allocation_type": knownvalue.StringExact("instance"),
							"instance_id":     knownvalue.StringExact(instance.ID()),
							"instance_name":   knownvalue.StringExact("web-1"),
							"status":          knownvalue.StringExact("allocated"),
							"interface_index": knownvalue.Int64Exact(1),
						}),
					})),
					statecheck.ExpectKnownValue(name, tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
					statecheck.ExpectKnownValue("data.onetsolutions_vpc_ip_allocations.empty", tfjsonpath.New("allocations"), knownvalue.ListSizeExact(0)),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_vpc_ip_allocations" "x" { vpc_id = "8a4c3c47-0000-4000-8000-000000000000" }`),
				ExpectError: regexp.MustCompile(`HTTP 404`),
			},
		},
	})
}
