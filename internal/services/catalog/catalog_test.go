package catalog_test

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

func TestInstanceSizeDataSources(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	var p4 fakeapi.Object
	for _, size := range f.All(fakeapi.KindSize) {
		if size["slug"] == "p-4" {
			p4 = size
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_instance_size" "by_slug" {
  slug = "p-4"
}
data "onetsolutions_instance_size" "by_id" {
  id = %q
}
data "onetsolutions_instance_sizes" "all" {}
data "onetsolutions_instance_sizes" "big_epyc" {
  min_vcpu   = 8
  min_ram_mb = 32768
  cpu_model  = "EPYC-9554"
}
`, p4.ID())),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_instance_size.by_slug", tfjsonpath.New("id"), knownvalue.StringExact(p4.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_size.by_slug", tfjsonpath.New("vcpu"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_size.by_slug", tfjsonpath.New("ram_mb"), knownvalue.Int64Exact(4096)),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_size.by_slug", tfjsonpath.New("volumes"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_size.by_slug",
						tfjsonpath.New("pricing"), knownvalue.ListPartial(map[int]knownvalue.Check{
							2: knownvalue.ObjectPartial(map[string]knownvalue.Check{
								"billing_cycle": knownvalue.StringExact("monthly"),
								"price":         knownvalue.StringExact("7.99"),
							}),
						})),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_size.by_slug",
						tfjsonpath.New("addons").AtSliceIndex(0).AtMapKey("slug"), knownvalue.StringExact("daily-backup")),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_size.by_id", tfjsonpath.New("slug"), knownvalue.StringExact("p-4")),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_sizes.all", tfjsonpath.New("sizes"), knownvalue.ListSizeExact(14)),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_sizes.big_epyc", tfjsonpath.New("sizes"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectPartial(map[string]knownvalue.Check{"slug": knownvalue.StringExact("hp-32-247")}),
						knownvalue.ObjectPartial(map[string]knownvalue.Check{"slug": knownvalue.StringExact("hp-64")}),
					})),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_instance_size" "x" { slug = "xxl" }`),
				ExpectError: regexp.MustCompile(`No instance size matches slug "xxl"`),
			},
			{
				Config: acctest.Config(f, `data "onetsolutions_instance_size" "x" { slug = "p-8" }`),
			},
		},
	})
}

func TestImageDataSources(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	private := f.SeedPrivateImage(scope, fakeapi.Object{"slug": "golden-debian", "created": "2026-11-01T00:00:00.000000Z"})
	var ubuntu fakeapi.Object
	for _, image := range f.All(fakeapi.KindPublicImage) {
		if image["slug"] == "ubuntu-2404" {
			ubuntu = image
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_image" "by_slug" {
  slug       = "ubuntu-2404"
  visibility = "public"
}
data "onetsolutions_image" "by_os" {
  os_name    = "ubuntu"
  os_version = "24.04"
}
data "onetsolutions_image" "windows_fr" {
  os_name    = "Windows"
  os_version = "2025"
  language   = "fr-FR"
}
data "onetsolutions_image" "debian_13_latest" {
  os_name     = "Debian"
  os_version  = "13"
  most_recent = true
}
data "onetsolutions_image" "private_by_id" {
  id = %q
}
data "onetsolutions_image" "public_by_id" {
  id         = %q
  visibility = "public"
}
data "onetsolutions_images" "debian" {
  os_name = "Debian"
}
data "onetsolutions_images" "private" {
  visibility = "private"
}
data "onetsolutions_images" "public" {
  visibility = "public"
}
`, private.ID(), ubuntu.ID())),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_image.by_slug", tfjsonpath.New("id"), knownvalue.StringExact(ubuntu.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_image.by_slug", tfjsonpath.New("organization_id"), knownvalue.Null()),
					statecheck.ExpectKnownValue("data.onetsolutions_image.by_os", tfjsonpath.New("slug"), knownvalue.StringExact("ubuntu-2404")),
					statecheck.ExpectKnownValue("data.onetsolutions_image.by_os", tfjsonpath.New("project_id"), knownvalue.StringExact(scope.ProjectID)),
					statecheck.ExpectKnownValue("data.onetsolutions_image.windows_fr", tfjsonpath.New("slug"), knownvalue.StringExact("windows-server-2025-fr-fr")),
					statecheck.ExpectKnownValue("data.onetsolutions_image.windows_fr", tfjsonpath.New("is_windows"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("data.onetsolutions_image.debian_13_latest", tfjsonpath.New("slug"), knownvalue.StringExact("golden-debian")),
					statecheck.ExpectKnownValue("data.onetsolutions_image.private_by_id", tfjsonpath.New("visibility"), knownvalue.StringExact("private")),
					statecheck.ExpectKnownValue("data.onetsolutions_image.public_by_id", tfjsonpath.New("os_name"), knownvalue.StringExact("Ubuntu")),
					statecheck.ExpectKnownValue("data.onetsolutions_images.debian", tfjsonpath.New("images"), knownvalue.ListSizeExact(4)),
					statecheck.ExpectKnownValue("data.onetsolutions_images.debian",
						tfjsonpath.New("images").AtSliceIndex(0).AtMapKey("slug"), knownvalue.StringExact("golden-debian")),
					statecheck.ExpectKnownValue("data.onetsolutions_images.private", tfjsonpath.New("images"), knownvalue.ListSizeExact(1)),
					statecheck.ExpectKnownValue("data.onetsolutions_images.public", tfjsonpath.New("images"), knownvalue.ListSizeExact(18)),
				},
			},
			{
				Config: acctest.Config(f, `
data "onetsolutions_image" "x" {
  os_name    = "Debian"
  os_version = "13"
}`),
				ExpectError: regexp.MustCompile(`2 images match os_name "Debian", os_version "13"`),
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_image" "x" { os_name = "Debian" }`),
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: acctest.Config(f, `
data "onetsolutions_image" "x" {
  slug     = "debian-13"
  language = "fr"
}`),
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_image" "x" {
  id         = %q
  visibility = "public"
}`, private.ID())),
				ExpectError: regexp.MustCompile(`No public image has id`),
			},
			{
				Config: acctest.Config(f, `data "onetsolutions_image" "x" { slug = "debian-13" }`),
			},
		},
	})
}

func TestControlPanelsAndHostingPlans(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, `
data "onetsolutions_control_panels" "all" {}
data "onetsolutions_hosting_plans" "all" {}
`),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_control_panels.all", tfjsonpath.New("control_panels"), knownvalue.ListSizeExact(7)),
					statecheck.ExpectKnownValue("data.onetsolutions_control_panels.all",
						tfjsonpath.New("control_panels").AtSliceIndex(0), knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"slug":                 knownvalue.StringExact("easypanel"),
							"access_port":          knownvalue.Int64Exact(3000),
							"compatible_image_ids": knownvalue.ListSizeExact(1),
						})),
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_plans.all", tfjsonpath.New("plans"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_plans.all",
						tfjsonpath.New("plans").AtSliceIndex(0), knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"slug":          knownvalue.StringExact("hebergement-business"),
							"disk_space_mb": knownvalue.Int64Exact(50000),
							"pricing":       knownvalue.ListSizeExact(3),
						})),
				},
			},
		},
	})
}

func TestWebHostingDataSource(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	scope := f.Scope()
	hosting := f.SeedWebHosting(scope, fakeapi.Object{"domain": "shop.example.fr", "dedicated_ip": "203.0.113.80"})
	f.SeedWebHosting(scope, fakeapi.Object{"domain": "old.example.fr", "status": "terminated"})
	f.SeedWebHosting(scope, fakeapi.Object{"domain": "old.example.fr"})
	f.SeedWebHosting(scope, fakeapi.Object{"domain": "twin.example.fr"})
	f.SeedWebHosting(scope, fakeapi.Object{"domain": "twin.example.fr"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_web_hosting" "by_domain" {
  domain = "shop.example.fr"
}
data "onetsolutions_web_hosting" "by_id" {
  id = %q
}
data "onetsolutions_web_hosting" "active_one" {
  domain = "old.example.fr"
}
`, hosting.ID())),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_web_hosting.by_domain", tfjsonpath.New("id"), knownvalue.StringExact(hosting.ID())),
					statecheck.ExpectKnownValue("data.onetsolutions_web_hosting.by_domain", tfjsonpath.New("dedicated_ip"), knownvalue.StringExact("203.0.113.80")),
					statecheck.ExpectKnownValue("data.onetsolutions_web_hosting.by_domain", tfjsonpath.New("plan_name"), knownvalue.StringExact("Hébergement Business")),
					statecheck.ExpectKnownValue("data.onetsolutions_web_hosting.by_domain", tfjsonpath.New("nameservers"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact("ns1.onetsolutions.net"), knownvalue.StringExact("ns2.onetsolutions.net"),
					})),
					statecheck.ExpectKnownValue("data.onetsolutions_web_hosting.by_id", tfjsonpath.New("domain"), knownvalue.StringExact("shop.example.fr")),
					statecheck.ExpectKnownValue("data.onetsolutions_web_hosting.active_one", tfjsonpath.New("status"), knownvalue.StringExact("active")),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_web_hosting" "x" { domain = "twin.example.fr" }`),
				ExpectError: regexp.MustCompile(`2 objects of type web hosting match domain "twin.example.fr"`),
			},
			{
				Config: acctest.Config(f, `data "onetsolutions_web_hosting" "x" { domain = "shop.example.fr" }`),
			},
		},
	})
}
