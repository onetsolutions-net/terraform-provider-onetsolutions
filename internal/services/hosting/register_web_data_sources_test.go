package hosting_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
)

func TestWebListDataSources(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hosting := seedHosting(f)
	hostingID, domain := hosting.ID(), hosting.String("domain")
	job := f.SeedHostingCronJob(f.Scope(), hostingID, fakeapi.Object{"hour": "3"})
	f.SeedHostingSubdomain(f.Scope(), hostingID, "blog")
	f.SeedHostingAddonDomain(f.Scope(), hostingID, "example.org")
	f.SeedHostingRedirect(f.Scope(), hostingID, fakeapi.Object{"type": "302"})
	site := f.SeedWordPressSite(f.Scope(), hostingID, fakeapi.Object{"path": "blog", "title": "Blog"})

	config := fmt.Sprintf(`
data "onetsolutions_hosting_cron_jobs" "test" {
  hosting_id = %[1]q
}

data "onetsolutions_hosting_subdomains" "test" {
  hosting_id = %[1]q
}

data "onetsolutions_hosting_addon_domains" "test" {
  hosting_id = %[1]q
}

data "onetsolutions_hosting_redirects" "test" {
  hosting_id = %[1]q
}

data "onetsolutions_hosting_wordpress_sites" "test" {
  hosting_id = %[1]q
}
`, hostingID)

	item := func(values map[string]string) knownvalue.Check {
		checks := map[string]knownvalue.Check{}
		for name, value := range values {
			checks[name] = knownvalue.StringExact(value)
		}
		return knownvalue.ListExact([]knownvalue.Check{knownvalue.ObjectExact(checks)})
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, config),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_cron_jobs.test", tfjsonpath.New("cron_jobs"), item(map[string]string{
						"line_key": job.ID(), "command": job.String("command"),
						"minute": "*", "hour": "3", "day": "*", "month": "*", "weekday": "*",
					})),
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_cron_jobs.test", tfjsonpath.New("project_id"), knownvalue.StringExact(f.Scope().ProjectID)),
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_subdomains.test", tfjsonpath.New("subdomains"), item(map[string]string{
						"subdomain": "blog", "domain": domain, "full_domain": "blog." + domain,
					})),
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_addon_domains.test", tfjsonpath.New("addon_domains"), item(map[string]string{
						"domain": "example.org", "subdomain": "example-org", "document_root": "/home/seeded/public_html/example.org",
					})),
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_redirects.test", tfjsonpath.New("redirects"), item(map[string]string{
						"domain": domain, "source": "/old", "destination": "https://example.com/", "type": "302",
					})),
					statecheck.ExpectKnownValue("data.onetsolutions_hosting_wordpress_sites.test", tfjsonpath.New("wordpress_sites"), item(map[string]string{
						"id": site.String("id"), "domain": domain, "path": "blog", "title": "Blog", "version": "6.5.0",
					})),
				},
			},
		},
	})
}

func TestWebListDataSources_unknownHosting(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_hosting_subdomains" "test" {
  hosting_id = %q
}
`, uuid.NewString())),
				ExpectError: regexp.MustCompile(`Could not list the subdomains of the web hosting`),
			},
		},
	})
}
