package compute_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
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
	instanceName       = "onetsolutions_instance.test"
	instancesRoute     = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances"
	instanceRoute      = instancesRoute + "/{id}"
	orderPreviewRoute  = instancesRoute + "/order-preview"
	resizeRoute        = instanceRoute + "/resize"
	resizePreviewRoute = instanceRoute + "/resize-preview"
	stopRoute          = instanceRoute + "/stop"
	startRoute         = instanceRoute + "/start"
	instanceTaskRoute  = instancesRoute + "/{instance_id}/tasks/{id}"
)

// The attributes the API never returns, so an import cannot read them back.
var createOnlyAttributes = []string{
	"ssh_key_ids", "volume_id", "control_panel", "one_click_app", "addon_ids", "accepted_contract_ids", "timeouts",
}

// catalog picks sizes of the captured catalog: base, a larger size it is eligible to, and a
// smaller one it is not.
type catalog struct {
	image, base, larger, smaller string
}

func sizesOf(t *testing.T, f *fakeapi.Server) catalog {
	t.Helper()
	bySlug := map[string]string{}
	for _, size := range f.All(fakeapi.KindSize) {
		bySlug[size.String("slug")] = size.ID()
	}
	c := catalog{image: f.First(fakeapi.KindPublicImage).ID(), base: bySlug["p-4"], larger: bySlug["p-8"], smaller: bySlug["gp-2-253"]}
	if c.base == "" || c.larger == "" || c.smaller == "" {
		t.Fatalf("catalog fixture lacks p-4, p-8 or gp-2-253: %v", bySlug)
	}
	return c
}

func newFake(t *testing.T, opts ...fakeapi.Option) *fakeapi.Server {
	t.Helper()
	return acctest.NewFake(t, opts...)
}

func instanceConfig(f *fakeapi.Server, c catalog, size, body string) string {
	return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_instance" "test" {
  name     = "tf-acc-web"
  size_id  = %q
  image_id = %q
%s
}
`, size, c.image, body))
}

func emptyPlanAfterApply() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
}

func expectAction(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(instanceName, action)},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func captureID(id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[instanceName]
		if !ok {
			return fmt.Errorf("%s not in state", instanceName)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func checkIDUnchanged(id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[instanceName]
		if !ok {
			return fmt.Errorf("%s not in state", instanceName)
		}
		if rs.Primary.ID != *id {
			return fmt.Errorf("%s was replaced: id %s, was %s", instanceName, rs.Primary.ID, *id)
		}
		return nil
	}
}

func checkFake(check func() error) resource.TestCheckFunc {
	return func(*terraform.State) error { return check() }
}

func countTasks(f *fakeapi.Server, taskType string) int {
	n := 0
	for _, task := range f.All(fakeapi.KindInstanceTask) {
		if task.String("task_type") == taskType {
			n++
		}
	}
	return n
}

func expectTasks(f *fakeapi.Server, taskType string, want int) resource.TestCheckFunc {
	return checkFake(func() error {
		if n := countTasks(f, taskType); n != want {
			return fmt.Errorf("%d %s tasks, want %d", n, taskType, want)
		}
		for _, task := range f.All(fakeapi.KindInstanceTask) {
			if task.String("task_type") == taskType && task.String("status") != "completed" {
				return fmt.Errorf("task %s (%s) is %s: the provider did not wait for it", task.ID(), taskType, task.String("status"))
			}
		}
		return nil
	})
}

func expectRequests(f *fakeapi.Server, method, route string, want int) resource.TestCheckFunc {
	return checkFake(func() error {
		if n := f.CountRequests(method, route); n != want {
			return fmt.Errorf("%s %s sent %d times, want %d", method, route, n, want)
		}
		return nil
	})
}

func TestInstanceResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	key := f.SeedSSHKey(f.Scope(), nil).ID()
	vpc := f.SeedVPC(f.Scope(), nil).ID()
	otherVPC := f.SeedVPC(f.Scope(), fakeapi.Object{"cidr_block": "10.11.0.0/16"}).ID()
	withKey := fmt.Sprintf("  ssh_key_ids = [%q]\n", key)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{
				Config:           instanceConfig(f, c, c.base, withKey+`  tags = ["web"]`),
				ConfigPlanChecks: emptyPlanAfterApply(),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("status"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("state"), knownvalue.StringExact("running")),
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("is_installed"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("billing_cycle"), knownvalue.StringExact("monthly")),
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("tags"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("web")})),
					statecheck.ExpectKnownValue(instanceName, tfjsonpath.New("primary_ipv4"), knownvalue.NotNull()),
				},
				Check: resource.ComposeTestCheckFunc(
					captureID(&id),
					checkFake(func() error {
						if f.CountRequests(http.MethodPost, orderPreviewRoute) == 0 {
							return fmt.Errorf("the plan did not preview the order price")
						}
						return nil
					}),
					expectRequests(f, http.MethodPost, instancesRoute, 1),
				),
			},
			{
				ResourceName:            instanceName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ImportIDFunc(instanceName),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: createOnlyAttributes,
			},
			{
				Config:           instanceConfig(f, c, c.base, withKey+`  tags = ["web"]`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionNoop),
			},
			{
				Config:           strings.Replace(instanceConfig(f, c, c.base, withKey+`  tags = ["web"]`), `"tf-acc-web"`, `"tf-acc-web-2"`, 1),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckResourceAttr(instanceName, "name", "tf-acc-web-2"),
					expectTasks(f, fakeapi.TaskChangeHostname, 1),
				),
			},
			{
				Config:           strings.Replace(instanceConfig(f, c, c.base, withKey+`  tags = ["web", "prod"]`), `"tf-acc-web"`, `"tf-acc-web-2"`, 1),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckResourceAttr(instanceName, "tags.#", "2"),
					expectTasks(f, fakeapi.TaskChangeHostname, 1),
				),
			},
			{
				Config:           strings.Replace(instanceConfig(f, c, c.base, withKey+fmt.Sprintf("  vpc_id = %q\n", vpc)), `"tf-acc-web"`, `"tf-acc-web-2"`, 1),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckResourceAttr(instanceName, "vpc_id", vpc),
					resource.TestCheckNoResourceAttr(instanceName, "tags"),
					expectTasks(f, fakeapi.TaskUpdateVPC, 1),
				),
			},
			{
				Config:           strings.Replace(instanceConfig(f, c, c.base, withKey+fmt.Sprintf("  vpc_id = %q\n", otherVPC)), `"tf-acc-web"`, `"tf-acc-web-2"`, 1),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(instanceName, "vpc_id", otherVPC),
					expectTasks(f, fakeapi.TaskUpdateVPC, 2),
				),
			},
			{
				Config:           strings.Replace(instanceConfig(f, c, c.base, withKey), `"tf-acc-web"`, `"tf-acc-web-2"`, 1),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckNoResourceAttr(instanceName, "vpc_id"),
					expectTasks(f, fakeapi.TaskUpdateVPC, 3),
				),
			},
		},
	})
}

// Plan section 5: an import followed by the original configuration plans nothing. The
// configuration sets no create-only input, which an import cannot read back.
func TestInstanceResource_importThenEmptyPlan(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	seeded := f.SeedInstance(f.Scope(), fakeapi.Object{
		"name": "tf-acc-imported", "size": mustGet(t, f, fakeapi.KindSize, c.base), "tags": []any{"imported"},
	})
	config := acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_instance" "test" {
  name     = "tf-acc-imported"
  size_id  = %q
  image_id = %q
  tags     = ["imported"]
}
`, c.base, c.image))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:          config,
				ResourceName:    instanceName,
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateId:   seeded.ID(),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(instanceName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config:             config,
				ResourceName:       instanceName,
				ImportState:        true,
				ImportStateId:      seeded.ID(),
				ImportStatePersist: true,
			},
			{
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				// A create-only input added after an import is stored without a replacement.
				Config: strings.Replace(config, `tags     = ["imported"]`,
					fmt.Sprintf(`tags = ["imported"]
  ssh_key_ids = [%q]`, f.SeedSSHKey(f.Scope(), nil).ID()), 1),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            resource.TestCheckResourceAttr(instanceName, "id", seeded.ID()),
			},
		},
	})
}

func tagList(n int) string {
	tags := make([]string, n)
	for i := range tags {
		tags[i] = fmt.Sprintf("%q", fmt.Sprintf("tag-%d", i))
	}
	return strings.Join(tags, ", ")
}

func mustGet(t *testing.T, f *fakeapi.Server, kind fakeapi.Kind, id string) fakeapi.Object {
	t.Helper()
	object, ok := f.Get(kind, id)
	if !ok {
		t.Fatalf("%s %s not found", kind, id)
	}
	return object
}

func TestInstanceResource_powerState(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{
				Config:           instanceConfig(f, c, c.base, `  state = "stopped"`),
				ConfigPlanChecks: emptyPlanAfterApply(),
				Check: resource.ComposeTestCheckFunc(
					captureID(&id),
					resource.TestCheckResourceAttr(instanceName, "state", "stopped"),
					expectTasks(f, fakeapi.TaskStopInstance, 1),
					checkVMStatus(f, &id, "stopped"),
				),
			},
			{
				Config:           instanceConfig(f, c, c.base, `  state = "running"`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckResourceAttr(instanceName, "state", "running"),
					expectTasks(f, fakeapi.TaskStartInstance, 1),
					checkVMStatus(f, &id, "running"),
				),
			},
			{
				Config:           instanceConfig(f, c, c.base, `  state = "stopped"`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            resource.ComposeTestCheckFunc(checkVMStatus(f, &id, "stopped"), expectTasks(f, fakeapi.TaskStopInstance, 2)),
			},
			{
				// Started outside Terraform: the next plan stops it again.
				PreConfig: func() {
					f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) { o["state_sync"] = fakeapi.Object{"vm_status": "running"} })
				},
				Config:           instanceConfig(f, c, c.base, `  state = "stopped"`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            checkVMStatus(f, &id, "stopped"),
			},
		},
	})
}

func checkVMStatus(f *fakeapi.Server, id *string, want string) resource.TestCheckFunc {
	return checkFake(func() error {
		instance, ok := f.Get(fakeapi.KindInstance, *id)
		if !ok {
			return fmt.Errorf("instance %s not in the fake", *id)
		}
		if got := instance.Map("state_sync").String("vm_status"); got != want {
			return fmt.Errorf("vm_status = %s, want %s", got, want)
		}
		return nil
	})
}

func checkSize(f *fakeapi.Server, id *string, sizeID, cycle string) resource.TestCheckFunc {
	return checkFake(func() error {
		instance, ok := f.Get(fakeapi.KindInstance, *id)
		if !ok {
			return fmt.Errorf("instance %s not in the fake", *id)
		}
		if instance.Map("size").ID() != sizeID || instance.String("billing_cycle") != cycle {
			return fmt.Errorf("size %s on %s, want %s on %s", instance.Map("size").ID(), instance.String("billing_cycle"), sizeID, cycle)
		}
		return nil
	})
}

func TestInstanceResource_resizeRefusedWithoutAllowStopping(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, "")},
			{
				Config:      instanceConfig(f, c, c.larger, ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Changing the size needs the instance stopped.*allow_stopping_for_update`),
			},
			{
				Config:           instanceConfig(f, c, c.base, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            expectRequests(f, http.MethodPost, resizeRoute, 0),
			},
		},
	})
}

func TestInstanceResource_resizeStopsAndStarts(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{
				Config: instanceConfig(f, c, c.base, `  allow_stopping_for_update = true`),
				Check:  captureID(&id),
			},
			{
				Config:           instanceConfig(f, c, c.larger, `  allow_stopping_for_update = true`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckResourceAttr(instanceName, "size_id", c.larger),
					resource.TestCheckResourceAttr(instanceName, "state", "running"),
					checkSize(f, &id, c.larger, "monthly"),
					checkVMStatus(f, &id, "running"),
					expectTasks(f, fakeapi.TaskStopInstance, 1),
					expectTasks(f, fakeapi.TaskStartInstance, 1),
					expectRequests(f, http.MethodPost, resizeRoute, 1),
					checkFake(func() error {
						if f.CountRequests(http.MethodPost, resizePreviewRoute) == 0 {
							return fmt.Errorf("the plan did not preview the resize price")
						}
						return nil
					}),
				),
			},
			{
				// An ineligible size is refused at plan time.
				Config:      instanceConfig(f, c, c.smaller, `  allow_stopping_for_update = true`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Size not eligible`),
			},
		},
	})
}

func TestInstanceResource_resizeStoppedInstanceStaysStopped(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, `  state = "stopped"`), Check: captureID(&id)},
			{
				Config:           instanceConfig(f, c, c.larger, `  state = "stopped"`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkSize(f, &id, c.larger, "monthly"),
					checkVMStatus(f, &id, "stopped"),
					expectTasks(f, fakeapi.TaskStartInstance, 0),
				),
			},
		},
	})
}

// The API's resize refuses the current size (SAME_SIZE), so a new billing cycle alone is refused
// at plan time; with a new size, the resize order applies both.
func TestInstanceResource_billingCycleChangesOnlyWithASize(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	stopped := `  billing_cycle = "%s"
  state         = "stopped"`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: instanceConfig(f, c, c.base, fmt.Sprintf(stopped, "monthly")),
				Check:  resource.ComposeTestCheckFunc(captureID(&id), resource.TestCheckResourceAttr(instanceName, "recurring_amount", "7.99")),
			},
			{
				Config:      instanceConfig(f, c, c.base, fmt.Sprintf(stopped, "annually")),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)The billing cycle cannot change alone.*"monthly"`),
			},
			{
				Config:           instanceConfig(f, c, c.larger, fmt.Sprintf(stopped, "annually")),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					checkSize(f, &id, c.larger, "annually"),
					resource.TestCheckResourceAttr(instanceName, "billing_cycle", "annually"),
					expectRequests(f, http.MethodPost, resizeRoute, 1),
				),
			},
		},
	})
}

// hourly cannot be ordered and volume_id is not applied by the order: both are refused at plan
// time, before anything is ordered.
func TestInstanceResource_unorderableInputsRefusedAtPlan(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      instanceConfig(f, c, c.base, `  billing_cycle = "hourly"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Billing cycle not orderable`),
			},
			{
				Config:      instanceConfig(f, c, c.base, fmt.Sprintf(`  volume_id = %q`, c.base)),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`volume_id cannot be set on a new instance`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, instancesRoute); n != 0 {
		t.Fatalf("ordered %d instances", n)
	}
}

// An instance billed hourly before hourly orders were closed imports and plans nothing.
func TestInstanceResource_importHourlyInstance(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	seeded := f.SeedInstance(f.Scope(), fakeapi.Object{
		"name": "tf-acc-hourly", "size": mustGet(t, f, fakeapi.KindSize, c.base), "billing_cycle": "hourly",
	})
	config := acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_instance" "test" {
  name          = "tf-acc-hourly"
  size_id       = %q
  image_id      = %q
  billing_cycle = "hourly"
}
`, c.base, c.image))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:          config,
			ResourceName:    instanceName,
			ImportState:     true,
			ImportStateKind: resource.ImportBlockWithID,
			ImportStateId:   seeded.ID(),
			ImportPlanChecks: resource.ImportPlanChecks{
				PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(instanceName, plancheck.ResourceActionNoop)},
			},
		}},
	})
}

// The order does not apply vpc_id or tags: the provider sets them once the instance is
// installed, so the result matches the plan.
func TestInstanceResource_orderGapsSetAfterInstall(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	vpc := f.SeedVPC(f.Scope(), nil).ID()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{{
			Config:           instanceConfig(f, c, c.base, fmt.Sprintf("  vpc_id = %q\n  tags = [\"web\", \"Prod\"]", vpc)),
			ConfigPlanChecks: emptyPlanAfterApply(),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr(instanceName, "vpc_id", vpc),
				resource.TestCheckResourceAttr(instanceName, "tags.#", "2"),
				expectRequests(f, http.MethodPatch, instanceRoute, 1),
				expectTasks(f, fakeapi.TaskUpdateVPC, 1),
				checkFake(func() error {
					instance := f.All(fakeapi.KindInstance)[0]
					if instance.Map("vpc").ID() != vpc || len(instance.List("tags")) != 2 {
						return fmt.Errorf("the API holds vpc %v and tags %v", instance["vpc"], instance["tags"])
					}
					return nil
				}),
			),
		}},
	})
}

// Instances of the same name ordered in parallel each get the instance of their own order, found
// by order_id, even with an instance of that name already in the project: no name matching, no
// lock between the creates.
func TestInstanceResource_sameNameOrdersFoundByOrder(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	existing := f.SeedInstance(f.Scope(), fakeapi.Object{"name": "tf-acc-worker"}).ID()
	config := acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_instance" "test" {
  count    = 3
  name     = "tf-acc-worker"
  size_id  = %q
  image_id = %q
}
`, c.base, c.image))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{{
			Config:           config,
			ConfigPlanChecks: emptyPlanAfterApply(),
			Check: func(s *terraform.State) error {
				ids := map[string]bool{}
				for i := range 3 {
					rs, ok := s.RootModule().Resources[fmt.Sprintf("%s.%d", instanceName, i)]
					if !ok {
						return fmt.Errorf("%s[%d] not in state", instanceName, i)
					}
					instance, ok := f.Get(fakeapi.KindInstance, rs.Primary.ID)
					if !ok || rs.Primary.ID == existing {
						return fmt.Errorf("%s[%d] holds %s, not one of the ordered instances", instanceName, i, rs.Primary.ID)
					}
					if orderID := rs.Primary.Attributes["order_id"]; orderID == "" || instance.String("order_id") != orderID {
						return fmt.Errorf("%s[%d]: order_id %q in state, %q in the API", instanceName, i, orderID, instance.String("order_id"))
					}
					ids[rs.Primary.ID] = true
				}
				if len(ids) != 3 || len(f.All(fakeapi.KindInstance)) != 4 {
					return fmt.Errorf("%d distinct IDs in state for %d instances", len(ids), len(f.All(fakeapi.KindInstance)))
				}
				return nil
			},
		}},
	})
}

// An older API version ignores ?order_id= and serves no order_id: the create fails with
// what to do, and adopts none of the instances listed, not even the one it ordered.
func TestInstanceResource_apiWithoutOrderLinkAdoptsNothing(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithoutOrderLink())
	c := sizesOf(t, f)
	f.SeedInstance(f.Scope(), fakeapi.Object{"name": "tf-acc-web"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: instanceConfig(f, c, c.base, ""),
			ExpectError: regexp.MustCompile(`(?s)The instance was ordered but could not be found.*did\s+not\s+report\s+` +
				`which\s+order.*cannot\s+identify.*adopts\s+none.*terraform\s+import\s+<address>\s+order:[0-9a-f-]{36}`),
		}},
		CheckDestroy: func(s *terraform.State) error {
			if n := len(s.RootModule().Resources); n != 0 {
				return fmt.Errorf("%d resources in the state, want none", n)
			}
			return nil
		},
	})
	if n := f.CountRequests(http.MethodPost, instancesRoute); n != 1 {
		t.Fatalf("ordered %d times, want 1", n)
	}
	if n := f.CountRequests(http.MethodGet, instancesRoute); n != 1 {
		t.Fatalf("listed the instances %d times, want 1: the provider kept waiting for a match that cannot come", n)
	}
}

// An order whose answer is lost after the API placed it (a gateway timeout, a dropped
// connection) gives no local order id to find the instance by: the create says an order may be
// placed and adopts nothing, not even the only instance of that name.
func TestInstanceResource_lostOrderAnswerAdoptsNothing(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	f.Inject(http.MethodPost, instancesRoute, fakeapi.Injection{Status: http.StatusGatewayTimeout, AfterHandling: true})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      instanceConfig(f, c, c.base, ""),
			ExpectError: regexp.MustCompile(`(?s)The instance may have been ordered.*check the\s+console.*import`),
		}},
		CheckDestroy: func(s *terraform.State) error {
			if n := len(s.RootModule().Resources); n != 0 {
				return fmt.Errorf("%d resources in the state, want none", n)
			}
			return nil
		},
	})
	if n := f.CountRequests(http.MethodPost, instancesRoute); n != 1 {
		t.Fatalf("ordered %d times, want 1", n)
	}
	if n := len(f.All(fakeapi.KindInstance)); n != 1 {
		t.Fatalf("%d instances in the fake, want the 1 the lost order placed", n)
	}
}

// A failed order request that may or may not have been placed says so, rather than inviting a
// plain retry that would order a second instance.
func TestInstanceResource_failedOrderRequestWarnsItMayBePlaced(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	f.Inject(http.MethodPost, instancesRoute, fakeapi.Injection{Status: http.StatusBadGateway})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: instanceConfig(f, c, c.base, `  timeouts {
    create = "1s"
  }`),
			ExpectError: regexp.MustCompile(`(?s)The instance may have been ordered.*check the\s+console`),
		}},
	})
	if n := f.CountRequests(http.MethodPost, instancesRoute); n != 1 {
		t.Fatalf("ordered %d times, want 1", n)
	}
}

// A resize whose order is never paid times out with the instance started again, and the next
// apply, which the API refuses (an earlier upgrade is unpaid), does not stop it again.
func TestInstanceResource_unpaidResizeRestartsTheInstance(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithUnpaidResizes())
	c := sizesOf(t, f)
	var id string
	resized := instanceConfig(f, c, c.larger, `  allow_stopping_for_update = true
  timeouts {
    update = "1s"
  }`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, `  allow_stopping_for_update = true`), Check: captureID(&id)},
			{
				Config:      resized,
				ExpectError: regexp.MustCompile(`(?s)Could not resize the instance.*timed out waiting for instance`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeTestCheckFunc(
					checkVMStatus(f, &id, "running"),
					resource.TestCheckResourceAttr(instanceName, "state", "running"),
					resource.TestCheckResourceAttr(instanceName, "size_id", c.base),
					expectTasks(f, fakeapi.TaskStopInstance, 1),
					expectTasks(f, fakeapi.TaskStartInstance, 1),
				),
			},
			{
				Config:      resized,
				ExpectError: regexp.MustCompile(`(?s)Could not resize the instance.*awaiting payment`),
				Check:       checkVMStatus(f, &id, "running"),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: resource.ComposeTestCheckFunc(
					checkVMStatus(f, &id, "running"),
					expectTasks(f, fakeapi.TaskStopInstance, 1),
				),
			},
			{Config: instanceConfig(f, c, c.base, `  allow_stopping_for_update = true`)},
		},
	})
}

// A new instance whose synced power state still reads stopped once installed is started, so the
// default state = "running" holds after apply.
func TestInstanceResource_createStartsAnInstanceReadStopped(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithInstalledStopped())
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:           instanceConfig(f, c, c.base, ""),
			ConfigPlanChecks: emptyPlanAfterApply(),
			Check: resource.ComposeTestCheckFunc(
				captureID(&id),
				resource.TestCheckResourceAttr(instanceName, "state", "running"),
				expectTasks(f, fakeapi.TaskStartInstance, 1),
				checkVMStatus(f, &id, "running"),
			),
		}},
	})
}

// An update that does not change state sends no power action, even when the API cannot tell
// whether the instance runs.
func TestInstanceResource_updateWithUnknownPowerStateSendsNoAction(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, ""), Check: captureID(&id)},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) { o["state_sync"] = fakeapi.Object{"vm_status": "unknown"} })
				},
				Config:           instanceConfig(f, c, c.base, `  tags = ["web"]`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(instanceName, "state", "running"),
					expectRequests(f, http.MethodPost, startRoute, 0),
					expectRequests(f, http.MethodPost, stopRoute, 0),
				),
			},
		},
	})
}

// locked, status and is_installed change at runtime (a backup locks the instance during an
// update), so an update plans them unknown instead of promising the prior value.
func TestInstanceResource_runtimeAttributesUnknownOnUpdate(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, "")},
			{
				Config: instanceConfig(f, c, c.base, `  tags = ["web"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue(instanceName, tfjsonpath.New("locked")),
					plancheck.ExpectUnknownValue(instanceName, tfjsonpath.New("status")),
					plancheck.ExpectUnknownValue(instanceName, tfjsonpath.New("is_installed")),
					plancheck.ExpectKnownValue(instanceName, tfjsonpath.New("created"), knownvalue.NotNull()),
				}},
			},
		},
	})
}

// checkCancellation checks the fake recorded the cancellation of the captured instance with
// reason code and reason, and kept it listed and active: a destroy terminates nothing.
func checkCancellation(f *fakeapi.Server, id *string, code, reason string) func() error {
	return func() error {
		instance, ok := f.Get(fakeapi.KindInstance, *id)
		switch {
		case !ok:
			return fmt.Errorf("instance %s was removed: a destroy must only cancel it", *id)
		case instance["cancellation_requested_at"] == nil || instance["billing_end_date"] == nil:
			return fmt.Errorf("instance %s has no cancellation requested: %v", *id, instance)
		case instance.String("cancellation_reason_code") != code || instance.String("cancellation_reason") != reason:
			return fmt.Errorf("cancellation recorded with %q / %q, want %q / %q", instance.String("cancellation_reason_code"),
				instance.String("cancellation_reason"), code, reason)
		case instance.String("status") != "active" || instance["is_active"] != true:
			return fmt.Errorf("instance %s is %s after the destroy, want still active", *id, instance.String("status"))
		}
		return nil
	}
}

// A destroy sends DELETE with the default reason code and the API's default sentence: the
// instance is cancelled at the end of its period, still listed and active, and leaves the state.
func TestInstanceResource_destroyRequestsCancellation(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	config := instanceConfig(f, c, c.base, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					captureID(&id),
					resource.TestCheckResourceAttr(instanceName, "cancellation_reason_code", "no_longer_needed"),
					resource.TestCheckNoResourceAttr(instanceName, "cancellation_reason"),
					resource.TestCheckNoResourceAttr(instanceName, "cancellation_requested_at"),
					resource.TestCheckNoResourceAttr(instanceName, "billing_end_date"),
				),
			},
			{
				Config:  config,
				Destroy: true,
				Check:   checkFake(checkCancellation(f, &id, "no_longer_needed", fakeapi.DeleteInstanceDefaultReason)),
			},
			{
				// The cancelled instance runs until the end of its period: it can be imported again.
				Config:             config,
				ResourceName:       instanceName,
				ImportState:        true,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return id, nil },
				ImportStatePersist: true,
			},
			{
				// Destroying it a second time is answered 202 unchanged: the first request stands.
				Config:  config,
				Destroy: true,
				Check: resource.ComposeTestCheckFunc(
					expectRequests(f, http.MethodDelete, instanceRoute, 2),
					checkFake(checkCancellation(f, &id, "no_longer_needed", fakeapi.DeleteInstanceDefaultReason)),
				),
			},
		},
	})
}

// An explicit reason code and reason are sent in the DELETE body.
func TestInstanceResource_destroySendsTheReason(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	const reason = "The service moves to another region next month."
	config := instanceConfig(f, c, c.base, fmt.Sprintf("  cancellation_reason_code = \"switching_provider\"\n  cancellation_reason = %q", reason))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, ""), Check: captureID(&id)},
			{
				// Changing the reason sends nothing: it is used by the destroy only.
				Config:           config,
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					expectRequests(f, http.MethodPatch, instanceRoute, 0),
					expectRequests(f, http.MethodDelete, instanceRoute, 0),
				),
			},
			{
				Config:  config,
				Destroy: true,
				Check:   checkFake(checkCancellation(f, &id, "switching_provider", reason)),
			},
		},
	})
}

// An instance whose cancellation was requested outside Terraform (the console) stays in the
// state with the dates of the cancellation and plans nothing; destroying it then sends a DELETE
// the API answers unchanged, and once the API reports it terminated it leaves the state.
func TestInstanceResource_cancellationRequestedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	const requestedAt, endDate = "2026-11-10T09:00:00Z", "2026-12-01T00:00:00Z"
	config := instanceConfig(f, c, c.base, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&id)},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) {
						o["cancellation_requested_at"] = requestedAt
						o["cancellation_reason_code"] = "too_expensive"
						o["cancellation_reason"] = "Requested from the console by the account owner."
						o["billing_end_date"] = endDate
					})
				},
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckResourceAttr(instanceName, "cancellation_requested_at", requestedAt),
					resource.TestCheckResourceAttr(instanceName, "billing_end_date", endDate),
					resource.TestCheckResourceAttr(instanceName, "status", "active"),
				),
			},
			{
				Config:  config,
				Destroy: true,
				Check: resource.ComposeTestCheckFunc(
					expectRequests(f, http.MethodDelete, instanceRoute, 1),
					checkFake(func() error {
						instance, _ := f.Get(fakeapi.KindInstance, id)
						if instance.String("cancellation_requested_at") != requestedAt || instance.String("cancellation_reason_code") != "too_expensive" {
							return fmt.Errorf("the earlier cancellation was overwritten: %v", instance)
						}
						return nil
					}),
				),
			},
		},
	})
}

// A revoked cancellation clears the dates, and the instance can be cancelled again.
func TestInstanceResource_revokedCancellationIsRead(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	config := instanceConfig(f, c, c.base, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&id)},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) {
						o["cancellation_requested_at"] = "2026-11-10T09:00:00Z"
						o["billing_end_date"] = "2026-12-01T00:00:00Z"
					})
				},
				RefreshState: true,
				Check:        resource.TestCheckResourceAttr(instanceName, "billing_end_date", "2026-12-01T00:00:00Z"),
			},
			{
				PreConfig: func() {
					if err := revokeCancellation(t, f, id); err != nil {
						t.Error(err)
					}
				},
				Config:           config,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr(instanceName, "cancellation_requested_at"),
					resource.TestCheckNoResourceAttr(instanceName, "billing_end_date"),
				),
			},
		},
	})
}

// revokeCancellation calls the fake's revoke-cancellation as the console does.
func revokeCancellation(t *testing.T, f *fakeapi.Server, id string) error {
	scope := f.Scope()
	url := fmt.Sprintf("%s/v1/organizations/%s/projects/%s/compute/instances/%s/revoke-cancellation",
		f.URL(), scope.OrganizationID, scope.ProjectID, id)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+f.APIKey())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke-cancellation answered %d", resp.StatusCode)
	}
	return nil
}

func TestInstanceResource_createOnlyChangeWarnsWithoutReplacing(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	first := f.SeedSSHKey(f.Scope(), nil).ID()
	second := f.SeedSSHKey(f.Scope(), nil).ID()
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: instanceConfig(f, c, c.base, fmt.Sprintf("  ssh_key_ids = [%q]\n  install_management_key = true", first)),
				Check:  captureID(&id),
			},
			{
				Config: instanceConfig(f, c, c.base, fmt.Sprintf(`  ssh_key_ids = [%q]
  install_management_key = false
  allow_password_reset   = false
  control_panel          = "plesk"
  one_click_app {
    slug       = "wordpress"
    parameters = { site_title = "Blog" }
  }`, second)),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					checkIDUnchanged(&id),
					resource.TestCheckResourceAttr(instanceName, "ssh_key_ids.0", second),
					resource.TestCheckResourceAttr(instanceName, "install_management_key", "false"),
					resource.TestCheckResourceAttr(instanceName, "one_click_app.0.slug", "wordpress"),
					expectRequests(f, http.MethodPost, instancesRoute, 1),
					expectRequests(f, http.MethodPatch, instanceRoute, 0),
				),
			},
		},
	})
}

func TestInstanceResource_imageChangeReplaces(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	images := f.All(fakeapi.KindPublicImage)
	other := images[len(images)-1].ID()
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, ""), Check: captureID(&id)},
			{
				Config:           strings.Replace(instanceConfig(f, c, c.base, ""), c.image, other, 1),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionDestroyBeforeCreate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(instanceName, "image_id", other),
					checkFake(func() error {
						old, ok := f.Get(fakeapi.KindInstance, id)
						if !ok || old["cancellation_requested_at"] == nil {
							return fmt.Errorf("the replaced instance %s was not cancelled: %v", id, old)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestInstanceResource_drift(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	recreate := resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(instanceName, plancheck.ResourceActionCreate)},
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, ""), Check: captureID(&id)},
			{
				PreConfig:        func() { f.Delete(fakeapi.KindInstance, id) },
				Config:           instanceConfig(f, c, c.base, ""),
				ConfigPlanChecks: recreate,
				Check:            captureID(&id),
			},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) { o["status"] = "terminated"; o["is_active"] = false })
				},
				Config:           instanceConfig(f, c, c.base, ""),
				ConfigPlanChecks: recreate,
			},
		},
	})
}

func TestInstanceResource_errorStatusTaintsAndKeepsTracking(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	f.FailNextTask(fakeapi.TaskCreateInstance, "installation failed")
	config := instanceConfig(f, c, c.base, "")
	var failedID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)Could not create the instance.*unexpected state "error"`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              checkTainted(f),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(instanceName, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				PreConfig: func() {
					all := f.All(fakeapi.KindInstance)
					if len(all) != 1 || all[0].String("status") != "error" {
						t.Errorf("want one instance in error, got %v", all)
						return
					}
					failedID = all[0].ID()
				},
				Check: checkFake(func() error {
					failed, ok := f.Get(fakeapi.KindInstance, failedID)
					if !ok || failed["cancellation_requested_at"] == nil {
						return fmt.Errorf("the tainted instance %s was not cancelled", failedID)
					}
					if n := len(f.All(fakeapi.KindInstance)); n != 2 {
						return fmt.Errorf("%d instances, want 2: the failed one, cancelled, and its replacement", n)
					}
					return nil
				}),
			},
		},
	})
}

// checkTainted verifies the only instance of the fake is in the state, marked tainted.
func checkTainted(f *fakeapi.Server) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		all := f.All(fakeapi.KindInstance)
		if len(all) != 1 {
			return fmt.Errorf("%d instances in the fake, want 1", len(all))
		}
		rs, ok := s.RootModule().Resources[instanceName]
		if !ok {
			return fmt.Errorf("the instance %s was dropped from the state", all[0].ID())
		}
		if rs.Primary.ID != all[0].ID() || !rs.Primary.Tainted {
			return fmt.Errorf("state holds %s (tainted %v), want %s tainted", rs.Primary.ID, rs.Primary.Tainted, all[0].ID())
		}
		return nil
	}
}

func TestInstanceResource_createTimeoutKeepsTracking(t *testing.T) {
	t.Parallel()
	f := newFake(t, fakeapi.WithTaskPolls(1_000_000))
	c := sizesOf(t, f)
	config := instanceConfig(f, c, c.base, `  timeouts {
    create = "2s"
  }`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?s)timed out waiting for instance.*stays in`),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              checkTainted(f),
			},
		},
		CheckDestroy: func(s *terraform.State) error {
			if n := f.CountRequests(http.MethodDelete, instanceRoute); n != 1 {
				return fmt.Errorf("DELETE sent %d times, want 1: the timed-out instance was orphaned", n)
			}
			if n := f.CountRequests(http.MethodPost, instancesRoute); n != 1 {
				return fmt.Errorf("ordered %d times, want 1", n)
			}
			return acctest.CheckInstancesCancelled(f)(s)
		},
	})
}

func TestInstanceResource_deletionProtection(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	protected := instanceConfig(f, c, c.base, `  deletion_protection = true`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{Config: protected, ConfigPlanChecks: emptyPlanAfterApply()},
			{
				Config:      protected,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`The instance is protected from deletion`),
			},
			{
				Config:      strings.Replace(protected, c.image, f.All(fakeapi.KindPublicImage)[1].ID(), 1),
				ExpectError: regexp.MustCompile(`The instance is protected from deletion`),
			},
			{
				Config:           instanceConfig(f, c, c.base, `  deletion_protection = false`),
				ConfigPlanChecks: expectAction(plancheck.ResourceActionUpdate),
				Check:            expectRequests(f, http.MethodDelete, instanceRoute, 0),
			},
		},
	})
}

// The API never refuses a cancellation while the instance is locked (a backup running): the
// destroy sends one DELETE and does not wait for the lock.
func TestInstanceResource_destroyWhileLocked(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	var id string
	config := instanceConfig(f, c, c.base, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckInstancesCancelled(f),
		Steps: []resource.TestStep{
			{Config: config, Check: captureID(&id)},
			{
				PreConfig: func() {
					f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) { o["locked"] = true; o["lock_reason"] = "backup" })
				},
				Config:  config,
				Destroy: true,
				Check: resource.ComposeTestCheckFunc(
					expectRequests(f, http.MethodDelete, instanceRoute, 1),
					checkFake(checkCancellation(f, &id, "no_longer_needed", fakeapi.DeleteInstanceDefaultReason)),
				),
			},
		},
	})
}

func TestInstanceResource_previewFailureNeverBlocksThePlan(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)
	f.Inject(http.MethodPost, orderPreviewRoute, fakeapi.Injection{Status: http.StatusBadRequest, Times: 100})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: instanceConfig(f, c, c.base, ""), ConfigPlanChecks: emptyPlanAfterApply()},
		},
	})
}

func TestInstanceResource_invalidInputsAtPlan(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	c := sizesOf(t, f)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      strings.Replace(instanceConfig(f, c, c.base, ""), `"tf-acc-web"`, `"-bad_name"`, 1),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be a hostname`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  billing_cycle = "weekly"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`billing_cycle`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  state = "paused"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`state`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  tags = ["Web", "web"]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Duplicate tag`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  tags = ["a/b"]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be 1 to 50 letters`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  tags = ["web "]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`must be 1 to 50 letters`),
			},
			{
				Config:      instanceConfig(f, c, c.base, fmt.Sprintf("  tags = [%s]", tagList(21))),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`at most 20`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  cancellation_reason_code = "bored"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`cancellation_reason_code`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  cancellation_reason = "Too expensive for us."`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)Reason too short.*4 words.*at least 7`),
			},
			{
				Config:      instanceConfig(f, c, c.base, `  cancellation_reason = " We no longer need this server for the project. "`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Surrounding whitespace`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, instancesRoute); n != 0 {
		t.Fatalf("ordered %d instances from invalid configurations", n)
	}
	if n := f.CountRequests(http.MethodGet, instanceTaskRoute); n != 0 {
		t.Fatalf("read %d tasks", n)
	}
}
