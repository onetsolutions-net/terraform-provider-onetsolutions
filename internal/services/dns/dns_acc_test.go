package dns_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
)

// envAccZoneName names a domain of the test project's organization the DNS acceptance test may
// create a zone for (and `make sweep` delete).
const envAccZoneName = "ONETSOLUTIONS_ACC_ZONE_NAME"

// TestAccDNSZone_records runs against the real API (TF_ACC=1, `make testacc`): it creates a zone
// and records, and destroys them.
func TestAccDNSZone_records(t *testing.T) {
	zone := os.Getenv(envAccZoneName)
	config := func(address string) string {
		return fmt.Sprintf(`
resource "onetsolutions_dns_zone" "test" {
  zone_name = %q
}
resource "onetsolutions_dns_record" "apex" {
  zone_id = onetsolutions_dns_zone.test.id
  type    = "A"
  name    = "@"
  content = %q
  ttl     = 300
}
resource "onetsolutions_dns_record" "txt" {
  zone_id = onetsolutions_dns_zone.test.id
  type    = "TXT"
  name    = "tf-acc"
  content = "v=spf1 -all"
}
`, zone, address)
	}
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			acctest.PreCheck(t)
			if zone == "" {
				t.Fatalf("%s must be set for the DNS acceptance test", envAccZoneName)
			}
		},
		ProtoV6ProviderFactories: acctest.RealProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:           config("192.0.2.10"),
				ConfigPlanChecks: emptyPlanAfterApply(),
				Check:            resource.TestCheckResourceAttrSet("onetsolutions_dns_zone.test", "nameservers.0"),
			},
			{
				ResourceName:      "onetsolutions_dns_record.apex",
				ImportState:       true,
				ImportStateIdFunc: acctest.ChildImportIDFunc("onetsolutions_dns_record.apex", "zone_id"),
				ImportStateVerify: true,
			},
			{
				Config: config("192.0.2.11"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("onetsolutions_dns_record.apex", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}
