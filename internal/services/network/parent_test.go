package network_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/network"
)

// cancelInstance makes an instance cancelled, as the console's cancellation does: its children
// stay readable, and every change to them is refused with 403.
func cancelInstance(f *fakeapi.Server, id string) {
	f.Mutate(fakeapi.KindInstance, id, func(o fakeapi.Object) {
		o["status"] = "cancelled"
		o["is_active"] = false
	})
}

// deleteThroughResource runs a resource's Delete with a state holding attributes, as a destroy
// planned before the instance was cancelled does (refresh disabled, or the cancellation landing
// between the refresh and the delete).
func deleteThroughResource(t *testing.T, f *fakeapi.Server, r resource.Resource, attributes map[string]string) resource.DeleteResponse {
	t.Helper()
	ctx := context.Background()
	c, err := client.New(client.Config{BaseURL: f.URL(), APIKey: f.APIKey(), RequestsPerSecond: 100, RetryMax: -1})
	if err != nil {
		t.Fatal(err)
	}
	configured, ok := r.(resource.ResourceWithConfigure)
	if !ok {
		t.Fatal("the resource takes no provider data")
	}
	configureResp := &resource.ConfigureResponse{}
	configured.Configure(ctx, resource.ConfigureRequest{ProviderData: &common.ProviderData{Client: c, Waiter: acctest.TestWaiter}}, configureResp)
	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	if configureResp.Diagnostics.HasError() || schemaResp.Diagnostics.HasError() {
		t.Fatal(configureResp.Diagnostics, schemaResp.Diagnostics)
	}
	state := tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil)}
	for name, value := range attributes {
		if diags := state.SetAttribute(ctx, path.Root(name), value); diags.HasError() {
			t.Fatal(diags)
		}
	}
	resp := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &resp)
	return resp
}

func scopeAttributes(f *fakeapi.Server, extra map[string]string) map[string]string {
	extra["organization_id"] = f.Scope().OrganizationID
	extra["project_id"] = f.Scope().ProjectID
	return extra
}

// Rules under a cancelled instance cannot be deleted (403): they are gone with the instance, so
// the destroy succeeds instead of failing on every attempt.
func TestInstanceFirewallDelete_cancelledInstanceIsAlreadyGone(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	f.SeedFirewallRule(f.Scope(), instanceID, fakeapi.Object{"name": "ssh", "priority": 100})
	cancelInstance(f, instanceID)

	resp := deleteThroughResource(t, f, network.NewInstanceFirewallResource(),
		scopeAttributes(f, map[string]string{"id": instanceID, "instance_id": instanceID}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("delete failed: %v", resp.Diagnostics)
	}
}

// A rule refused for another reason (the instance is still active, the caller lacks the role)
// is still an error: only a gone instance makes a 403 mean "already deleted".
func TestInstanceFirewallDelete_forbiddenOnAnActiveInstanceFails(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	f.SeedFirewallRule(f.Scope(), instanceID, fakeapi.Object{"name": "ssh", "priority": 100})
	f.Mutate(fakeapi.KindInstance, instanceID, func(o fakeapi.Object) { o["status"] = "suspended" })

	resp := deleteThroughResource(t, f, network.NewInstanceFirewallResource(),
		scopeAttributes(f, map[string]string{"id": instanceID, "instance_id": instanceID}))
	if !resp.Diagnostics.HasError() {
		t.Fatal("deleting the rules of a suspended instance succeeded, want the API's refusal")
	}
}

func TestInstanceSnapshotDelete_cancelledInstanceIsAlreadyGone(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instanceID := seedInstance(f).ID()
	snapshot := f.SeedSnapshot(f.Scope(), instanceID, fakeapi.Object{"name": "before-upgrade"})
	cancelInstance(f, instanceID)

	resp := deleteThroughResource(t, f, network.NewInstanceSnapshotResource(),
		scopeAttributes(f, map[string]string{"id": snapshot.ID(), "instance_id": instanceID, "name": "before-upgrade"}))
	if resp.Diagnostics.HasError() {
		t.Fatalf("delete failed: %v", resp.Diagnostics)
	}
}

// Once the instance is cancelled, the refresh forgets its firewall, snapshots and reverse DNS: the API
// still lists them, but nothing about them can change any more, and keeping them would make every
// later plan try (and fail) to change or delete them.
func TestInstanceChildren_forgottenOnceTheInstanceIsCancelled(t *testing.T) {
	t.Parallel()
	f := acctest.NewFake(t)
	instance := seedInstance(f)
	instanceID := instance.ID()
	addressID, _ := primaryAddress(f, instance)
	config := acctest.Config(f, firewallConfig(instanceID, sshRule)+snapshotConfig(instanceID, "tf-acc-cancelled", "")+
		reverseDNSConfig(instanceID, fmt.Sprintf("ip_address_id = %q", addressID), "web.example.com"))

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: acctest.ProtoV6ProviderFactories,
		Steps: []tfresource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { cancelInstance(f, instanceID) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					for _, name := range []string{firewallName, snapshotName, reverseDNSName} {
						if _, kept := s.RootModule().Resources[name]; kept {
							return fmt.Errorf("%s is still in the state after its instance was cancelled", name)
						}
					}
					return nil
				},
			},
		},
	})
}
