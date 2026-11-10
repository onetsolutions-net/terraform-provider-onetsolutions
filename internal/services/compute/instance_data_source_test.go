package compute_test

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

func TestInstanceDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	vpc := f.SeedVPC(f.Scope(), nil).ID()
	seeded := f.SeedInstance(f.Scope(), fakeapi.Object{"name": "db-1", "tags": []any{"db"}, "vpc_id": vpc})
	f.SeedInstance(f.Scope(), fakeapi.Object{"name": "twin"})
	f.SeedInstance(f.Scope(), fakeapi.Object{"name": "twin"})
	f.SeedInstance(f.Scope(), fakeapi.Object{"name": "gone", "status": "terminated", "is_active": false})

	byID, byName := "data.onetsolutions_instance.by_id", "data.onetsolutions_instance.by_name"
	checks := func(name string) []statecheck.StateCheck {
		return []statecheck.StateCheck{
			statecheck.ExpectKnownValue(name, tfjsonpath.New("id"), knownvalue.StringExact(seeded.ID())),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("name"), knownvalue.StringExact("db-1")),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("vpc_id"), knownvalue.StringExact(vpc)),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("state"), knownvalue.StringExact("running")),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("status"), knownvalue.StringExact("active")),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("size_id"), knownvalue.StringExact(f.First(fakeapi.KindSize).ID())),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("tags"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("db")})),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("primary_ipv4"), knownvalue.StringExact(seeded.String("primary_ipv4"))),
			statecheck.ExpectKnownValue(name, tfjsonpath.New("ip_addresses"), knownvalue.ListSizeExact(1)),
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_instance" "by_id" {
  id = %q
}

data "onetsolutions_instance" "by_name" {
  name = "db-1"
}
`, seeded.ID())),
				ConfigStateChecks: append(checks(byID), checks(byName)...),
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_instance" "x" { name = "twin" }`),
				ExpectError: regexp.MustCompile(`Several instances found`),
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_instance" "x" { name = "gone" }`),
				ExpectError: regexp.MustCompile(`No instance found`),
			},
		},
	})
}
