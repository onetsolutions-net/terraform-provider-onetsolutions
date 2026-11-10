package waiter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/acctest/fakeapi"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var fast = waiter.Config{MinInterval: time.Millisecond, MaxInterval: 2 * time.Millisecond}

func newClient(t *testing.T, f *fakeapi.Server) *client.Client {
	t.Helper()
	c, err := client.New(client.Config{BaseURL: f.URL(), APIKey: f.APIKey(), RequestsPerSecond: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInstanceTaskCompletes(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t, fakeapi.WithTaskPolls(3))
	scope := f.Scope()
	instance := f.SeedInstance(scope, nil)
	task := f.SeedInstanceTask(scope, instance.ID(), "apply_firewall")

	done, err := waiter.InstanceTask(context.Background(), fast, newClient(t, f),
		client.Scope(scope), instance.ID(), task.ID())
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != client.TaskCompleted || done.TaskType != "apply_firewall" {
		t.Fatalf("task = %+v", done)
	}
}

func TestInstanceTaskFailure(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	scope := f.Scope()
	instance := f.SeedInstance(scope, nil)
	f.FailNextTask("create_snapshot", "not enough space")
	task := f.SeedInstanceTask(scope, instance.ID(), "create_snapshot")

	_, err := waiter.InstanceTask(context.Background(), fast, newClient(t, f),
		client.Scope(scope), instance.ID(), task.ID())
	var failed *waiter.TaskFailedError
	if !errors.As(err, &failed) || failed.Status != client.TaskFailed || failed.Message != "not enough space" {
		t.Fatalf("err = %v", err)
	}
}

func TestDomainTaskCompletes(t *testing.T) {
	t.Parallel()
	f := fakeapi.New(t)
	scope := f.Scope()
	domain := f.SeedDomain(scope, nil)
	task := f.SeedDomainTask(scope, domain.ID(), "update_nameservers")

	done, err := waiter.DomainTask(context.Background(), fast, newClient(t, f),
		client.Scope(scope), domain.ID(), task.ID())
	if err != nil || done.Status != client.TaskCompleted {
		t.Fatalf("task = %+v, err = %v", done, err)
	}
}
