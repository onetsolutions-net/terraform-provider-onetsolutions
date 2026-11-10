package provider_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/provider"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

func TestSchemaIsValid(t *testing.T) {
	t.Parallel()
	p := provider.New("test")()
	resp := &fwprovider.SchemaResponse{}
	p.Schema(context.Background(), fwprovider.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatal(diags)
	}
}

func TestMissingAPIKey(t *testing.T) {
	t.Setenv(common.EnvAPIKey, "")
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "onetsolutions" {
  api_url = %q
}
data "onetsolutions_organizations" "all" {}
`, f.URL()),
				ExpectError: regexp.MustCompile(`(?s)Missing API key.*ONETSOLUTIONS_API_KEY`),
			},
		},
	})
}

func TestSettingsFromEnvironment(t *testing.T) {
	f := acctest.NewFake(t)
	scope := f.Scope()
	t.Setenv(common.EnvAPIKey, f.APIKey())
	t.Setenv(common.EnvAPIURL, f.URL())
	t.Setenv(common.EnvOrganizationID, scope.OrganizationID)
	t.Setenv(common.EnvProjectID, scope.ProjectID)
	t.Setenv(common.EnvMaxRequestsPerSecond, "50")
	key := f.SeedSSHKey(scope, nil)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "onetsolutions_projects" "all" {}
data "onetsolutions_ssh_key" "key" {
  name = %q
}
`, key.String("name")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_projects.all", tfjsonpath.New("organization_id"),
						knownvalue.StringExact(scope.OrganizationID)),
					statecheck.ExpectKnownValue("data.onetsolutions_ssh_key.key", tfjsonpath.New("project_id"),
						knownvalue.StringExact(scope.ProjectID)),
					statecheck.ExpectKnownValue("data.onetsolutions_ssh_key.key", tfjsonpath.New("id"),
						knownvalue.StringExact(key.ID())),
				},
			},
		},
	})
}

// The rate limit read from the environment is validated, which proves the variable is read.
func TestInvalidRateLimitFromEnvironment(t *testing.T) {
	f := acctest.NewFake(t)
	t.Setenv(common.EnvAPIKey, f.APIKey())
	t.Setenv(common.EnvAPIURL, f.URL())
	for _, value := range []string{"0", "abc", "101"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(common.EnvMaxRequestsPerSecond, value)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      `data "onetsolutions_organizations" "all" {}`,
					ExpectError: regexp.MustCompile(`ONETSOLUTIONS_MAX_REQUESTS_PER_SECOND="` + value + `" must be an integer`),
				}},
			})
		})
	}
}

// http is refused for any host but a loopback one, from the configuration and from the
// environment, so the API key never travels in cleartext.
func TestCleartextURLRefused(t *testing.T) {
	t.Setenv(common.EnvAPIKey, "sk-onetsolutions-unused")
	t.Setenv(common.EnvAPIURL, "http://api.onetsolutions.net")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `provider "onetsolutions" { api_url = "http://api.onetsolutions.net" }` + "\n" + `data "onetsolutions_organizations" "all" {}`,
				ExpectError: regexp.MustCompile(`http\s+is\s+allowed\s+only\s+for\s+localhost`),
			},
			{
				Config:      `data "onetsolutions_organizations" "all" {}`,
				ExpectError: regexp.MustCompile(`http\s+is\s+allowed\s+only\s+for\s+localhost`),
			},
		},
	})
}

func TestInvalidSettings(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `provider "onetsolutions" { api_url = "api.onetsolutions.net" }` + "\n" + `data "onetsolutions_organizations" "all" {}`,
				ExpectError: regexp.MustCompile(`invalid API URL`),
			},
			{
				Config: fmt.Sprintf(`
provider "onetsolutions" {
  api_url         = %q
  api_key         = %q
  organization_id = "not-a-uuid"
}
data "onetsolutions_organizations" "all" {}
`, f.URL(), f.APIKey()),
				ExpectError: regexp.MustCompile(`lowercase UUID`),
			},
			{
				Config: fmt.Sprintf(`
provider "onetsolutions" {
  api_url                 = %q
  api_key                 = %q
  max_requests_per_second = 0
}
data "onetsolutions_organizations" "all" {}
`, f.URL(), f.APIKey()),
				ExpectError: regexp.MustCompile(`max_requests_per_second`),
			},
		},
	})
}

func TestWrongAPIKeyShowsTheAPIMessage(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "onetsolutions" {
  api_url = %q
  api_key = "sk-onetsolutions-wrong"
}
data "onetsolutions_organizations" "all" {}
`, f.URL()),
				ExpectError: regexp.MustCompile(`(?s)Authentication credentials were not provided.*HTTP 401`),
			},
		},
	})
}

// A provider configured from another resource's output is unknown during the first plan: the
// provider defers the error and works once the value is known at apply.
func TestUnknownSettingsAreDeferred(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	key, _ := fakeapi.NewPublicKey("")
	scope := f.Scope()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "terraform_data" "key" {
  input = %q
}
provider "onetsolutions" {
  api_url         = %q
  api_key         = terraform_data.key.output
  organization_id = %q
  project_id      = %q
}
resource "onetsolutions_ssh_key" "test" {
  name       = "tf-acc-deferred"
  public_key = %q
}
`, f.APIKey(), f.URL(), scope.OrganizationID, scope.ProjectID, key),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("onetsolutions_ssh_key.test", tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
				},
			},
		},
	})
}
