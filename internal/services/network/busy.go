package network

import (
	"context"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

// instanceBusy matches the API's refusals while a task holds the instance or the instance is
// locked: both are a 409, and every change to an instance's firewall, snapshots or addresses
// queues a task that makes the next change wait.
func instanceBusy(err error) bool {
	return client.IsConflict(err)
}

// retryWhileBusy runs op until the instance accepts it, the error is not a 409, or ctx ends.
func retryWhileBusy(ctx context.Context, cfg waiter.Config, description string, op func(context.Context) error) error {
	return waiter.Retry(ctx, cfg, description, op, instanceBusy)
}
