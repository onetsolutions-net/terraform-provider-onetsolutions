package hosting_test

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

const (
	cronJobName          = "onetsolutions_hosting_cron_job.test"
	cronJobCommand       = "php -q /home/seeded/public_html/cron.php"
	cronJobCreateRoute   = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/cron-jobs/create"
	cronJobDeleteRoute   = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}/cron-jobs/delete"
	cronJobDefaultFields = `minute = "*/5"`
)

func cronJobConfig(hostingID, fields string) string {
	return fmt.Sprintf(`
resource "onetsolutions_hosting_cron_job" "test" {
  hosting_id = %q
  command    = %q
  %s
}
`, hostingID, cronJobCommand, fields)
}

func checkCronJob(f *fakeapi.Server, field, want string) func(*terraform.State) error {
	return func(*terraform.State) error {
		jobs := f.All(fakeapi.KindHostingCronJob)
		if len(jobs) != 1 {
			return fmt.Errorf("the API has %d cron jobs, want 1", len(jobs))
		}
		if got := jobs[0].String(field); got != want {
			return fmt.Errorf("cron job %s in the API is %q, want %q", field, got, want)
		}
		return nil
	}
}

func TestCronJobResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	var firstKey string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_hosting_cron_job", fakeapi.KindHostingCronJob),
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, cronJobConfig(hostingID, cronJobDefaultFields)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(cronJobName, tfjsonpath.New("minute"), knownvalue.StringExact("*/5")),
					statecheck.ExpectKnownValue(cronJobName, tfjsonpath.New("hour"), knownvalue.StringExact("*")),
					statecheck.ExpectKnownValue(cronJobName, tfjsonpath.New("organization_id"), knownvalue.StringExact(f.Scope().OrganizationID)),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					checkCronJob(f, "minute", "*/5"),
					func(s *terraform.State) error {
						id, err := stateID(s, cronJobName)
						if err != nil {
							return err
						}
						if id == "" || !f.Exists(fakeapi.KindHostingCronJob, id) {
							return fmt.Errorf("id %q is not the line key of the job", id)
						}
						firstKey = id
						return nil
					},
				),
			},
			{
				ResourceName:      cronJobName,
				ImportState:       true,
				ImportStateIdFunc: acctest.ChildImportIDFunc(cronJobName, "hosting_id"),
				ImportStateVerify: true,
			},
			{
				ResourceName:      cronJobName,
				ImportState:       true,
				ImportStateIdFunc: shortImportID(cronJobName),
				ImportStateVerify: true,
			},
			{
				ResourceName:      cronJobName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(cronJobName, "hosting_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(cronJobName, plancheck.ResourceActionNoop)},
				},
			},
			{
				// The crontab changed around the job: it is found again by its command and schedule.
				PreConfig: func() {
					job, _ := f.Get(fakeapi.KindHostingCronJob, firstKey)
					f.Delete(fakeapi.KindHostingCronJob, firstKey)
					delete(job, "id")
					f.SeedHostingCronJob(f.Scope(), hostingID, job)
				},
				Config: acctest.Config(f, cronJobConfig(hostingID, cronJobDefaultFields)),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(s *terraform.State) error {
					id, err := stateID(s, cronJobName)
					if err != nil {
						return err
					}
					if id == firstKey || !f.Exists(fakeapi.KindHostingCronJob, id) {
						return fmt.Errorf("id %q is not the new line key of the job", id)
					}
					return nil
				},
			},
			{
				Config: acctest.Config(f, cronJobConfig(hostingID, cronJobDefaultFields+"\n  hour = \"3\"")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(cronJobName, plancheck.ResourceActionDestroyBeforeCreate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: checkCronJob(f, "hour", "3"),
			},
		},
	})
}

func TestCronJobResource_deletedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	config := acctest.Config(f, cronJobConfig(hostingID, ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					for _, job := range f.All(fakeapi.KindHostingCronJob) {
						f.Delete(fakeapi.KindHostingCronJob, job.ID())
					}
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(cronJobName, plancheck.ResourceActionCreate)},
				},
				Check: checkCronJob(f, "command", cronJobCommand),
			},
		},
	})
}

func TestCronJobResource_hostingGone(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	config := acctest.Config(f, cronJobConfig(hostingID, ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				// A terminated hosting's jobs are forgotten: the next plan wants a new one, and
				// destroying sends no delete.
				PreConfig:          func() { terminateHosting(f, hostingID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if n := f.CountRequests(http.MethodPost, cronJobDeleteRoute); n != 0 {
				return fmt.Errorf("delete sent %d times for a terminated hosting", n)
			}
			return nil
		},
	})
}

func TestCronJobResource_errors(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	hostingID := seedHosting(f).ID()
	f.SeedHostingCronJob(f.Scope(), hostingID, fakeapi.Object{"command": cronJobCommand, "minute": "*/5"})
	pending := f.SeedWebHosting(f.Scope(), fakeapi.Object{"status": "pending"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      acctest.Config(f, cronJobConfig(hostingID, `minute = "*/5 * * * *"`)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be one cron value`),
			},
			{
				Config:      acctest.Config(f, cronJobConfig(hostingID, cronJobDefaultFields)),
				ExpectError: regexp.MustCompile(`Cron job already exists`),
			},
			{
				Config:      acctest.Config(f, cronJobConfig(pending.ID(), "")),
				ExpectError: regexp.MustCompile(`hosting account is not active`),
			},
			{
				Config: acctest.Config(f, cronJobConfig(hostingID, "")),
				Check: func(*terraform.State) error {
					if n := f.CountRequests(http.MethodPost, cronJobCreateRoute); n != 2 {
						return fmt.Errorf("create sent %d times, want 2 (the pending hosting, then this one)", n)
					}
					return nil
				},
			},
		},
	})
}
