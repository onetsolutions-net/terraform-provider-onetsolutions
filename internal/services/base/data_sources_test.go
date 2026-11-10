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

func TestSSHKeyDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	key := f.SeedSSHKey(scope, fakeapi.Object{"name": "deploy"})
	f.SeedSSHKey(scope, fakeapi.Object{"name": "twin"})
	f.SeedSSHKey(scope, fakeapi.Object{"name": "twin"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, `data "onetsolutions_ssh_key" "x" {}`),
				ExpectError: regexp.MustCompile(`(?s)Exactly one of these attributes must be configured`),
			},
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_ssh_key" "by_name" {
  name = "deploy"
}
data "onetsolutions_ssh_key" "by_id" {
  id = %q
}
`, key["id"])),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_ssh_key.by_name", tfjsonpath.New("id"), knownvalue.StringExact(key.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_ssh_key.by_name", tfjsonpath.New("fingerprint"), knownvalue.StringExact(key.String("fingerprint"))),
					statecheck.ExpectKnownValue("data.onetsolutions_ssh_key.by_id", tfjsonpath.New("name"), knownvalue.StringExact("deploy")),
					statecheck.ExpectKnownValue("data.onetsolutions_ssh_key.by_id", tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_ssh_key" "x" { name = "missing" }`),
				ExpectError: regexp.MustCompile(`No SSH key matches name "missing"`),
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_ssh_key" "x" { name = "twin" }`),
				ExpectError: regexp.MustCompile(`2 objects of type SSH key match name "twin"`),
			},
		},
	})
}

func TestVPCDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	vpc := f.SeedVPC(f.Scope(), fakeapi.Object{"name": "backend", "cidr_block": "10.1.0.0/16"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_vpc" "by_name" {
  name = "backend"
}
data "onetsolutions_vpc" "by_id" {
  id = %q
}
`, vpc["id"])),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_vpc.by_name", tfjsonpath.New("id"), knownvalue.StringExact(vpc.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_vpc.by_name", tfjsonpath.New("cidr_block"), knownvalue.StringExact("10.1.0.0/16")),
					statecheck.ExpectKnownValue("data.onetsolutions_vpc.by_id", tfjsonpath.New("name"), knownvalue.StringExact("backend")),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_vpc" "x" { id = "8a4c3c47-0000-4000-8000-000000000000" }`),
				ExpectError: regexp.MustCompile(`HTTP 404`),
			},
		},
	})
}
