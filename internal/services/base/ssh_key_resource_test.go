package base_test

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

const sshKeyName = "onetsolutions_ssh_key.test"

func sshKeyConfig(name, publicKey string) string {
	return fmt.Sprintf(`
resource "onetsolutions_ssh_key" "test" {
  name       = %q
  public_key = %q
}
`, name, publicKey)
}

func TestSSHKeyResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	key, fingerprint := fakeapi.NewPublicKey("laptop")
	otherKey, _ := fakeapi.NewPublicKey("desktop")
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_ssh_key", fakeapi.KindSSHKey),
		Steps: []resource.TestStep{
			{
				// file() keeps the trailing newline: semantic equality keeps it out of the diff.
				Config: acctest.Config(f, sshKeyConfig("tf-acc-key", key+"\n")),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(sshKeyName, tfjsonpath.New("public_key"), knownvalue.StringExact(key+"\n")),
					statecheck.ExpectKnownValue(sshKeyName, tfjsonpath.New("fingerprint"), knownvalue.StringExact(fingerprint)),
					statecheck.ExpectKnownValue(sshKeyName, tfjsonpath.New("organization_id"), knownvalue.StringExact(scope.OrganizationID)),
					statecheck.ExpectKnownValue(sshKeyName, tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(sshKeyName, "id"),
					resource.TestCheckResourceAttrSet(sshKeyName, "created"),
					captureID(sshKeyName, &firstID),
					func(*terraform.State) error {
						// The API trims the key, so the state keeps the configured spelling only
						// through semantic equality.
						stored, _ := f.Get(fakeapi.KindSSHKey, firstID)
						if stored.String("public_key") != key {
							return fmt.Errorf("the API stored %q, want the trimmed key", stored.String("public_key"))
						}
						return nil
					},
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      sshKeyName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ImportIDFunc(sshKeyName),
				ImportStateVerify: true,
				// An import reads the API's trimmed key, not the configured trailing newline; the
				// next step proves the original configuration then plans nothing.
				ImportStateVerifyIgnore: []string{"public_key"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["public_key"] != key {
						return fmt.Errorf("imported public_key = %q, want the API's trimmed key", states[0].Attributes["public_key"])
					}
					return nil
				},
			},
			{
				// The API returns the key trimmed, so the configuration that plans nothing after
				// an import is the trimmed key; with file()'s newline the plan is a no-op update.
				Config:          acctest.Config(f, sshKeyConfig("tf-acc-key", key)),
				ResourceName:    sshKeyName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources[sshKeyName].Primary.ID, nil
				},
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(sshKeyName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// Only surrounding whitespace changes: an in-place no-op, never a replacement.
				Config: acctest.Config(f, sshKeyConfig("tf-acc-key", "  "+key)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(sshKeyName, plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkIDUnchanged(sshKeyName, &firstID),
			},
			{
				Config: acctest.Config(f, sshKeyConfig("tf-acc-key", otherKey)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(sshKeyName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
			{
				Config: acctest.Config(f, sshKeyConfig("tf-acc-renamed", otherKey)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(sshKeyName, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}

func TestSSHKeyResource_disappears(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	key, _ := fakeapi.NewPublicKey("")
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, sshKeyConfig("tf-acc-drift", key)),
				Check: resource.ComposeTestCheckFunc(
					captureID(sshKeyName, &id),
					func(*terraform.State) error {
						if !f.Delete(fakeapi.KindSSHKey, id) {
							return fmt.Errorf("SSH key %s not in the fake", id)
						}
						return nil
					},
				),
				ExpectNonEmptyPlan: true,
			},
			{
				Config: acctest.Config(f, sshKeyConfig("tf-acc-drift", key)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(sshKeyName, plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}

func TestSSHKeyResource_apiValidationError(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	key, _ := fakeapi.NewPublicKey("")
	f.SeedSSHKey(f.Scope(), fakeapi.Object{"name": "taken"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, sshKeyConfig("taken", key)),
				ExpectError: regexp.MustCompile(`SSH key with name 'taken' already exists`),
			},
		},
	})
}

func TestSSHKeyResource_invalidPublicKeyAtPlan(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, sshKeyConfig("bad", "not a key")),
				ExpectError: regexp.MustCompile(`must be an OpenSSH public key`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys"); n != 0 {
		t.Fatalf("an invalid key reached the API %d times", n)
	}
}

func TestSSHKeyResource_createIsNeverRetried(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	key, _ := fakeapi.NewPublicKey("")
	const route = "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys"
	f.Inject(http.MethodPost, route, fakeapi.Injection{Status: http.StatusBadGateway})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, sshKeyConfig("tf-acc-502", key)),
				ExpectError: regexp.MustCompile(`HTTP 502`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, route); n != 1 {
		t.Fatalf("create sent %d times after a 502, want exactly 1", n)
	}
}

func TestSSHKeyResource_throttledCreateIsRetried(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	key, _ := fakeapi.NewPublicKey("")
	const route = "/v1/organizations/{organization_id}/projects/{project_id}/compute/ssh-keys"
	f.Inject(http.MethodPost, route, fakeapi.Injection{Status: http.StatusTooManyRequests, RetryAfter: "0"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: acctest.Config(f, sshKeyConfig("tf-acc-429", key))},
		},
	})
	if n := f.CountRequests(http.MethodPost, route); n != 2 {
		t.Fatalf("create sent %d times after a 429, want 2", n)
	}
}

func captureID(resourceName string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s not in state", resourceName)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func checkIDUnchanged(resourceName string, id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("%s not in state", resourceName)
		}
		if rs.Primary.ID != *id {
			return fmt.Errorf("%s was replaced: id %s, was %s", resourceName, rs.Primary.ID, *id)
		}
		return nil
	}
}
