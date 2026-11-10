package catalog_test

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
)

// TestAccCatalog_dataSources reads the real catalog (TF_ACC=1, `make testacc`). It orders nothing.
func TestAccCatalog_dataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.RealProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
data "onetsolutions_instance_sizes" "all" {}
data "onetsolutions_images" "public" {
  visibility = "public"
}
data "onetsolutions_control_panels" "all" {}
data "onetsolutions_hosting_plans" "all" {}
data "onetsolutions_instance_size" "first" {
  slug = data.onetsolutions_instance_sizes.all.sizes[0].slug
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.onetsolutions_instance_size.first", "id"),
					resource.TestCheckResourceAttrSet("data.onetsolutions_images.public", "images.0.id"),
					resource.TestCheckResourceAttrSet("data.onetsolutions_control_panels.all", "control_panels.0.slug"),
					resource.TestCheckResourceAttrSet("data.onetsolutions_hosting_plans.all", "plans.0.id"),
				),
			},
		},
	})
}
