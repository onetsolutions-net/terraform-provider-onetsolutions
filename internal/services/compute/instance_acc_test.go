package compute_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
)

// Environment of the acceptance tests, on top of acctest.PreCheck's credentials: a size billed
// monthly (hourly cannot be ordered), a larger size the first one is eligible to, and an image.
const (
	envAccSizeID       = "ONETSOLUTIONS_ACC_SIZE_ID"
	envAccLargerSizeID = "ONETSOLUTIONS_ACC_LARGER_SIZE_ID"
	envAccImageID      = "ONETSOLUTIONS_ACC_IMAGE_ID"
)

func accPreCheck(t *testing.T) {
	t.Helper()
	acctest.PreCheck(t)
	for _, name := range []string{envAccSizeID, envAccLargerSizeID, envAccImageID} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s must be set for the instance acceptance tests", name)
		}
	}
}

func accInstanceConfig(sizeID, extra string) string {
	return fmt.Sprintf(`
resource "onetsolutions_instance" "test" {
  name                      = "tf-acc-instance"
  size_id                   = %q
  image_id                  = %q
  billing_cycle             = "monthly"
  allow_stopping_for_update = true
%s
}
`, sizeID, os.Getenv(envAccImageID), extra)
}

// TestAccInstance_lifecycle runs against the real API (TF_ACC=1, `make testacc`): it ORDERS a
// billed instance. Run it by hand only, never in CI.
func TestAccInstance_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: acctest.RealProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           accInstanceConfig(os.Getenv(envAccSizeID), `  tags = ["tf-acc"]`),
				ConfigPlanChecks: emptyPlanAfterApply(),
			},
			{
				ResourceName:            instanceName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ImportIDFunc(instanceName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: createOnlyAttributes,
			},
			{
				Config:           accInstanceConfig(os.Getenv(envAccSizeID), `  tags = ["tf-acc", "renamed"]`+"\n  state = \"stopped\""),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
			},
			{
				Config:           accInstanceConfig(os.Getenv(envAccLargerSizeID), `  tags = ["tf-acc", "renamed"]`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
			},
		},
	})
}
