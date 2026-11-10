// Package acctest holds what every provider test shares: the provider factories, the provider
// block pointing at the fake API, import ID helpers and the pre-checks of the real acceptance
// tests.
package acctest

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/provider"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

// ProviderName is the provider's local name in test configurations.
const ProviderName = "onetsolutions"

// TestWaiter polls every few milliseconds, so waits against the fake API are instant.
var TestWaiter = waiter.Config{MinInterval: 5 * time.Millisecond, MaxInterval: 20 * time.Millisecond, Factor: 2}

// ProtoV6ProviderFactories serves the provider in-process with test tuning: fast polling and
// fast retries.
var ProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	ProviderName: providerserver.NewProtocol6WithError(provider.New("test",
		provider.WithWaiter(TestWaiter),
		provider.WithRetryWait(time.Millisecond, 10*time.Millisecond),
	)()),
}

// RealProviderFactories serve the provider with its production polling and retries, for the
// acceptance tests against the real API: the test tuning above would poll it every few
// milliseconds.
var RealProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	ProviderName: providerserver.NewProtocol6WithError(provider.New("acc")()),
}

// NewFake starts a fake API for one test.
func NewFake(t *testing.T, opts ...fakeapi.Option) *fakeapi.Server {
	t.Helper()
	return fakeapi.New(t, opts...)
}

// ProviderConfig returns a provider block pointing at the fake, with its default organization
// and project as provider defaults.
func ProviderConfig(f *fakeapi.Server) string {
	return ProviderConfigWithScope(f, f.Scope())
}

// ProviderConfigWithScope returns a provider block pointing at the fake with the given defaults.
func ProviderConfigWithScope(f *fakeapi.Server, scope fakeapi.Scope) string {
	return fmt.Sprintf(`
provider "onetsolutions" {
  api_url                 = %q
  api_key                 = %q
  organization_id         = %q
  project_id              = %q
  max_requests_per_second = 100
}
`, f.URL(), f.APIKey(), scope.OrganizationID, scope.ProjectID)
}

// ProviderConfigWithoutDefaults returns a provider block pointing at the fake with no default
// organization or project, to exercise their lazy resolution.
func ProviderConfigWithoutDefaults(f *fakeapi.Server) string {
	return fmt.Sprintf(`
provider "onetsolutions" {
  api_url                 = %q
  api_key                 = %q
  max_requests_per_second = 100
}
`, f.URL(), f.APIKey())
}

// Config joins the provider block and the test's resources.
func Config(f *fakeapi.Server, body ...string) string {
	return ProviderConfig(f) + strings.Join(body, "\n")
}

// ImportIDFunc returns "<organization_id>/<project_id>/<id>" of a resource in state.
func ImportIDFunc(resourceName string) func(*terraform.State) (string, error) {
	return func(state *terraform.State) (string, error) {
		attrs, err := primaryAttributes(state, resourceName)
		if err != nil {
			return "", err
		}
		return strings.Join([]string{attrs[common.AttrOrganizationID], attrs[common.AttrProjectID], attrs[common.AttrID]}, "/"), nil
	}
}

// ChildImportIDFunc returns "<organization_id>/<project_id>/<parent_id>/<id>" of a child
// resource in state, the parent ID being read from parentAttribute.
func ChildImportIDFunc(resourceName, parentAttribute string) func(*terraform.State) (string, error) {
	return func(state *terraform.State) (string, error) {
		attrs, err := primaryAttributes(state, resourceName)
		if err != nil {
			return "", err
		}
		return strings.Join([]string{
			attrs[common.AttrOrganizationID], attrs[common.AttrProjectID], attrs[parentAttribute], attrs[common.AttrID],
		}, "/"), nil
	}
}

// CheckDestroyed verifies that no object of kind behind a resource of resourceType remains in
// the fake once the test destroys everything.
func CheckDestroyed(f *fakeapi.Server, resourceType string, kind fakeapi.Kind) func(*terraform.State) error {
	return func(state *terraform.State) error {
		for _, rs := range state.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			if f.Exists(kind, rs.Primary.ID) {
				return fmt.Errorf("%s %s still exists in the fake API", resourceType, rs.Primary.ID)
			}
		}
		return nil
	}
}

// CheckInstancesCancelled is the CheckDestroy of onetsolutions_instance: a destroy does not
// delete an instance, it requests its cancellation at the end of the billing period, so every
// instance the state held must still exist in the fake with a cancellation requested.
func CheckInstancesCancelled(f *fakeapi.Server) func(*terraform.State) error {
	return func(state *terraform.State) error {
		for _, rs := range state.RootModule().Resources {
			if rs.Type != "onetsolutions_instance" {
				continue
			}
			instance, ok := f.Get(fakeapi.KindInstance, rs.Primary.ID)
			if !ok {
				return fmt.Errorf("instance %s was removed from the fake API: a destroy must only cancel it", rs.Primary.ID)
			}
			if instance["cancellation_requested_at"] == nil {
				return fmt.Errorf("instance %s has no cancellation requested after the destroy", rs.Primary.ID)
			}
		}
		return nil
	}
}

// PreCheck guards the real acceptance tests (TF_ACC=1, run by `make testacc`): they order billed
// resources and need credentials for a test project.
func PreCheck(t *testing.T) {
	t.Helper()
	for _, name := range []string{common.EnvAPIKey, common.EnvOrganizationID, common.EnvProjectID} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s must be set for acceptance tests", name)
		}
	}
}

func primaryAttributes(state *terraform.State, resourceName string) (map[string]string, error) {
	rs, ok := state.RootModule().Resources[resourceName]
	if !ok {
		return nil, fmt.Errorf("resource %s not found in state", resourceName)
	}
	return rs.Primary.Attributes, nil
}
