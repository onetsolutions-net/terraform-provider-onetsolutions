package network

import (
	"context"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// instanceGone reports whether the instance a firewall, snapshot or address belongs to is gone
// for its owner: not found, or cancelled or terminated. The API still serves its children for
// reading, but refuses every change to them (403), so they can only be
// forgotten.
func instanceGone(ctx context.Context, c *client.Client, scope client.Scope, instanceID string) (bool, error) {
	instance, err := c.GetInstance(ctx, scope, instanceID)
	if client.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return instance.Gone(), nil
}

// goneWithInstance reports whether a failed change to a child object means the object is gone:
// a 404, or a 403 because its instance is cancelled or terminated. The 403's code
// (permission_denied) does not say why, so the instance is read to tell it from a missing role.
func goneWithInstance(ctx context.Context, c *client.Client, scope client.Scope, instanceID string, err error) bool {
	if client.IsNotFound(err) {
		return true
	}
	if !client.IsForbidden(err) {
		return false
	}
	gone, readErr := instanceGone(ctx, c, scope, instanceID)
	return readErr == nil && gone
}
