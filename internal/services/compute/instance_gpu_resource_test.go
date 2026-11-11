package compute_test

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
	gpuName        = "onetsolutions_instance_gpu.test"
	gpuAttachRoute = instanceRoute + "/gpus/attach"
	gpuDetachRoute = instanceRoute + "/gpus/detach"
	modelL4        = "NVIDIA L4"
	modelA100      = "NVIDIA A100"
)

// gpuHost seeds an instance whose host holds an L4 and an A100.
func gpuHost(f *fakeapi.Server) string {
	instanceID := f.SeedInstance(f.Scope(), nil).ID()
	f.SeedGPU(f.Scope(), instanceID, nil)
	f.SeedGPU(f.Scope(), instanceID, fakeapi.Object{
		"model": modelA100, "hourly_rate": "2.1000", "vram_gb": 80, "memory_type": "HBM2e", "architecture": "Ampere",
	})
	return instanceID
}

func gpuConfig(f *fakeapi.Server, instanceID, model, extra string) string {
	return acctest.Config(f, fmt.Sprintf(`
resource "onetsolutions_instance_gpu" "test" {
  instance_id = %q
  model       = %q
%s
}
`, instanceID, model, extra))
}

func captureGPU(id *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[gpuName]
		if !ok {
			return fmt.Errorf("%s not in state", gpuName)
		}
		*id = rs.Primary.ID
		return nil
	}
}

func gpuAction(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpuName, action)},
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func TestInstanceGPUResource_lifecycle(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_gpu", fakeapi.KindGPUAttachment),
		Steps: []resource.TestStep{
			{
				Config:           gpuConfig(f, instanceID, modelL4, ""),
				ConfigPlanChecks: gpuAction(plancheck.ResourceActionCreate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("status"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("vendor"), knownvalue.StringExact("nvidia")),
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("hourly_rate"), knownvalue.StringExact("0.7000")),
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("vram_gb"), knownvalue.Int64Exact(24)),
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("attached_at"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("deletion_protection"), knownvalue.Bool(false)),
				},
				Check: resource.ComposeTestCheckFunc(captureGPU(&firstID), expectTasks(f, fakeapi.TaskAttachGPU, 1)),
			},
			{
				ResourceName:            gpuName,
				ImportState:             true,
				ImportStateIdFunc:       acctest.ChildImportIDFunc(gpuName, "instance_id"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			{
				ResourceName:      gpuName,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: acctest.ChildImportIDFunc(gpuName, "instance_id"),
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpuName, plancheck.ResourceActionNoop)},
				},
			},
			{
				Config:           gpuConfig(f, instanceID, modelL4, "  deletion_protection = true"),
				ConfigPlanChecks: gpuAction(plancheck.ResourceActionUpdate),
				Check:            resource.TestCheckResourceAttrPtr(gpuName, "id", &firstID),
			},
			{
				Config:      gpuConfig(f, instanceID, modelA100, "  deletion_protection = true"),
				ExpectError: regexp.MustCompile(`protected\s+from\s+deletion`),
			},
			{
				Config:           gpuConfig(f, instanceID, modelL4, ""),
				ConfigPlanChecks: gpuAction(plancheck.ResourceActionUpdate),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPtr(gpuName, "id", &firstID),
					expectRequests(f, http.MethodPost, gpuDetachRoute, 0),
				),
			},
			{
				Config:           gpuConfig(f, instanceID, modelA100, ""),
				ConfigPlanChecks: gpuAction(plancheck.ResourceActionDestroyBeforeCreate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("hourly_rate"), knownvalue.StringExact("2.1000")),
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("vram_gb"), knownvalue.Int64Exact(80)),
				},
				Check: resource.ComposeTestCheckFunc(
					expectTasks(f, fakeapi.TaskDetachGPU, 1),
					expectTasks(f, fakeapi.TaskAttachGPU, 2),
					checkFake(func() error {
						if f.Exists(fakeapi.KindGPUAttachment, firstID) {
							return fmt.Errorf("the replaced attachment %s is still listed", firstID)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestInstanceGPUResource_detachedOutsideTerraform(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	config := gpuConfig(f, instanceID, modelL4, "")
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_gpu", fakeapi.KindGPUAttachment),
		Steps: []resource.TestStep{
			{Config: config, Check: captureGPU(&id)},
			{
				PreConfig:        func() { f.Delete(fakeapi.KindGPUAttachment, id) },
				Config:           config,
				ConfigPlanChecks: gpuAction(plancheck.ResourceActionCreate),
				Check: func(s *terraform.State) error {
					if s.RootModule().Resources[gpuName].Primary.ID == id {
						return fmt.Errorf("the attachment kept id %s, want a new one", id)
					}
					return nil
				},
			},
		},
	})
}

// An instance that no longer exists takes its GPU with it: the attachment leaves the state.
func TestInstanceGPUResource_instanceGone(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	config := gpuConfig(f, instanceID, modelL4, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { f.Delete(fakeapi.KindInstance, instanceID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestInstanceGPUResource_unavailableModelRefusedAtPlan(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      gpuConfig(f, instanceID, "NVIDIA H100", ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)No GPU of model "NVIDIA H100".*Models\s+available:\s+"NVIDIA\s+A100",\s+"NVIDIA\s+L4"`),
			},
		},
	})
}

// A failed attachment is kept in the state until the next refresh, which finds it ended: the
// next apply attaches a GPU again, and never a second one beside it.
func TestInstanceGPUResource_failedAttachIsTracked(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	f.FailNextTask(fakeapi.TaskAttachGPU, "the instance could not be restarted")
	config := gpuConfig(f, instanceID, modelL4, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_gpu", fakeapi.KindGPUAttachment),
		Steps: []resource.TestStep{
			{Config: config, ExpectError: regexp.MustCompile(`could\s+not\s+be\s+restarted`)},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(gpuName, plancheck.ResourceActionCreate)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("status"), knownvalue.StringExact("active")),
				},
				Check: resource.ComposeTestCheckFunc(
					expectRequests(f, http.MethodPost, gpuDetachRoute, 0),
					checkFake(func() error {
						if n := len(f.All(fakeapi.KindGPUAttachment)); n != 1 {
							return fmt.Errorf("%d attachments, want 1", n)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestInstanceGPUResource_retriesWhileBusy(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	f.SeedInstanceTask(f.Scope(), instanceID, "reboot")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_gpu", fakeapi.KindGPUAttachment),
		Steps: []resource.TestStep{
			{
				Config: gpuConfig(f, instanceID, modelL4, ""),
				Check: checkFake(func() error {
					if n := f.CountRequests(http.MethodPost, gpuAttachRoute); n < 2 {
						return fmt.Errorf("attach sent %d times, want a retry after the refusal", n)
					}
					return nil
				}),
			},
		},
	})
}

// An attach whose answer is lost after the API committed it is found on the instance and
// tracked: the GPU is not left billed outside Terraform, and no second one is requested.
func TestInstanceGPUResource_lostAttachAnswerIsTracked(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	f.Inject(http.MethodPost, gpuAttachRoute, fakeapi.Injection{Status: http.StatusBadGateway, AfterHandling: true})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_gpu", fakeapi.KindGPUAttachment),
		Steps: []resource.TestStep{
			{
				Config:           gpuConfig(f, instanceID, modelL4, ""),
				ConfigPlanChecks: gpuAction(plancheck.ResourceActionCreate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpuName, tfjsonpath.New("status"), knownvalue.StringExact("active")),
				},
				Check: resource.ComposeTestCheckFunc(
					expectRequests(f, http.MethodPost, gpuAttachRoute, 1),
					checkFake(func() error {
						if n := len(f.All(fakeapi.KindGPUAttachment)); n != 1 {
							return fmt.Errorf("%d attachments, want 1", n)
						}
						return nil
					}),
				),
			},
		},
	})
}

// An attach that failed before the API committed it adds nothing to the state and says what to
// check, since the provider cannot tell it from a lost answer.
func TestInstanceGPUResource_failedAttachRequest(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	f.Inject(http.MethodPost, gpuAttachRoute, fakeapi.Injection{Status: http.StatusBadGateway})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		CheckDestroy:             acctest.CheckDestroyed(f, "onetsolutions_instance_gpu", fakeapi.KindGPUAttachment),
		Steps: []resource.TestStep{
			{
				Config:      gpuConfig(f, instanceID, modelL4, ""),
				ExpectError: regexp.MustCompile(`The GPU may have been attached`),
				Check:       expectRequests(f, http.MethodPost, gpuAttachRoute, 1),
			},
		},
	})
}

// A refusal other than a busy instance is final: it is not retried until the timeout.
func TestInstanceGPUResource_finalConflictNotRetried(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	f.Inject(http.MethodPost, gpuAttachRoute, fakeapi.Injection{Status: http.StatusConflict, Body: fakeapi.Object{
		"detail": "This instance has never finished installing.", "code": "instance_not_provisioned",
	}})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      gpuConfig(f, instanceID, modelL4, ""),
				ExpectError: regexp.MustCompile(`never\s+finished\s+installing`),
			},
		},
	})
	if n := f.CountRequests(http.MethodPost, gpuAttachRoute); n != 1 {
		t.Fatalf("attach sent %d times, want 1", n)
	}
}

func TestInstanceGPUResource_invalidInputsAtPlan(t *testing.T) {
	t.Parallel()
	f := newFake(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      gpuConfig(f, "not-a-uuid", modelL4, ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not\s+a\s+lowercase\s+UUID`),
			},
			{
				Config:      gpuConfig(f, f.SeedInstance(f.Scope(), nil).ID(), "", ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`length\s+must\s+be\s+between\s+1\s+and\s+255`),
			},
		},
	})
}

func TestInstanceAvailableGPUsDataSource(t *testing.T) {
	t.Parallel()
	f := newFake(t)
	instanceID := gpuHost(f)
	bare := f.SeedInstance(f.Scope(), nil).ID()
	const name = "data.onetsolutions_instance_available_gpus.test"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_instance_available_gpus" "test" {
  instance_id = %q
}
data "onetsolutions_instance_available_gpus" "bare" {
  instance_id = %q
}
`, instanceID, bare)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(name, tfjsonpath.New("gpus"), knownvalue.ListSizeExact(2)),
					statecheck.ExpectKnownValue(name, tfjsonpath.New("gpus").AtSliceIndex(0).AtMapKey("model"), knownvalue.StringExact(modelA100)),
					statecheck.ExpectKnownValue(name, tfjsonpath.New("gpus").AtSliceIndex(0).AtMapKey("vram_gb"), knownvalue.Int64Exact(80)),
					statecheck.ExpectKnownValue(name, tfjsonpath.New("gpus").AtSliceIndex(1).AtMapKey("model"), knownvalue.StringExact(modelL4)),
					statecheck.ExpectKnownValue(name, tfjsonpath.New("gpus").AtSliceIndex(1).AtMapKey("hourly_rate"), knownvalue.StringExact("0.7000")),
					statecheck.ExpectKnownValue(name, tfjsonpath.New("project_id"), knownvalue.StringExact(f.Scope().ProjectID)),
					statecheck.ExpectKnownValue("data.onetsolutions_instance_available_gpus.bare", tfjsonpath.New("gpus"), knownvalue.ListSizeExact(0)),
				},
			},
			{
				// A GPU in use is no longer offered.
				PreConfig: func() {
					for _, gpu := range f.All(fakeapi.KindGPU) {
						if gpu.String("model") == modelL4 {
							f.SeedGPUAttachment(f.Scope(), instanceID, gpu.ID())
						}
					}
				},
				Config: acctest.Config(f, fmt.Sprintf(`
data "onetsolutions_instance_available_gpus" "test" {
  instance_id = %q
}
`, instanceID)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(name, tfjsonpath.New("gpus"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectPartial(map[string]knownvalue.Check{"model": knownvalue.StringExact(modelA100)}),
					})),
				},
			},
			{
				Config:      acctest.Config(f, `data "onetsolutions_instance_available_gpus" "x" { instance_id = "8a4c3c47-0000-4000-8000-000000000000" }`),
				ExpectError: regexp.MustCompile(`HTTP 404`),
			},
		},
	})
}
