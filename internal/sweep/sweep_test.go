// Package sweep deletes what interrupted acceptance runs left in the test project: every
// instance, SSH key and VPC named "tf-acc-*", and the DNS zone named by
// ONETSOLUTIONS_ACC_ZONE_NAME. An instance is not deleted: DELETE requests its cancellation at
// the end of the billing period, so a swept instance keeps running, billed, until then (and a VPC
// it is attached to cannot be swept before). It runs only when asked, against the REAL API:
//
//	go test ./internal/sweep -v -sweep=all
//
// with the credentials of `make testacc`. A plain `go test` runs nothing here.
package sweep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

const (
	namePrefix     = "tf-acc-"
	envAccZoneName = "ONETSOLUTIONS_ACC_ZONE_NAME"
	sweepTimeout   = 30 * time.Minute
)

func TestMain(m *testing.M) {
	resource.TestMain(m)
}

func init() {
	resource.AddTestSweepers("onetsolutions_instance", &resource.Sweeper{Name: "onetsolutions_instance", F: sweepInstances})
	resource.AddTestSweepers("onetsolutions_ssh_key", &resource.Sweeper{Name: "onetsolutions_ssh_key", F: sweepSSHKeys})
	resource.AddTestSweepers("onetsolutions_vpc", &resource.Sweeper{
		Name: "onetsolutions_vpc", F: sweepVPCs, Dependencies: []string{"onetsolutions_instance"},
	})
	resource.AddTestSweepers("onetsolutions_dns_zone", &resource.Sweeper{Name: "onetsolutions_dns_zone", F: sweepDNSZones})
}

func sweepClient() (*client.Client, client.Scope, error) {
	scope := client.Scope{
		OrganizationID: os.Getenv(common.EnvOrganizationID),
		ProjectID:      os.Getenv(common.EnvProjectID),
	}
	apiKey := os.Getenv(common.EnvAPIKey)
	if apiKey == "" || scope.OrganizationID == "" || scope.ProjectID == "" {
		return nil, scope, fmt.Errorf("%s, %s and %s must be set to sweep", common.EnvAPIKey, common.EnvOrganizationID, common.EnvProjectID)
	}
	c, err := client.New(client.Config{
		BaseURL:   os.Getenv(common.EnvAPIURL),
		APIKey:    apiKey,
		UserAgent: client.UserAgent("sweeper", ""),
	})
	return c, scope, err
}

// sweep deletes every listed object whose name matches.
func sweep[T any](list func(context.Context, *client.Client, client.Scope) ([]T, error), name func(T) string,
	matches func(string) bool, remove func(context.Context, *client.Client, client.Scope, T) error,
) error {
	c, scope, err := sweepClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()
	items, err := list(ctx, c, scope)
	if err != nil {
		return err
	}
	var errs []error
	for _, item := range items {
		if !matches(name(item)) {
			continue
		}
		if err := remove(ctx, c, scope, item); err != nil && !client.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("%s: %w", name(item), err))
		}
	}
	return errors.Join(errs...)
}

func testName(name string) bool { return strings.HasPrefix(name, namePrefix) }

func sweepInstances(string) error {
	return sweep(
		func(ctx context.Context, c *client.Client, s client.Scope) ([]client.Instance, error) {
			return c.ListActiveInstances(ctx, s)
		},
		func(i client.Instance) string { return i.Name },
		testName,
		func(ctx context.Context, c *client.Client, s client.Scope, i client.Instance) error {
			if i.CancellationRequestedAt != nil {
				return nil
			}
			_, err := c.DeleteInstance(ctx, s, i.ID, client.InstanceDelete{
				ReasonCode: client.DefaultCancellationReasonCode,
				Reason:     "Left behind by an interrupted acceptance test run of the Terraform provider.",
			})
			return err
		},
	)
}

func sweepSSHKeys(string) error {
	return sweep(
		func(ctx context.Context, c *client.Client, s client.Scope) ([]client.SSHKey, error) {
			return c.ListSSHKeys(ctx, s)
		},
		func(k client.SSHKey) string { return k.Name },
		testName,
		func(ctx context.Context, c *client.Client, s client.Scope, k client.SSHKey) error {
			return c.DeleteSSHKey(ctx, s, k.ID)
		},
	)
}

func sweepVPCs(string) error {
	return sweep(
		func(ctx context.Context, c *client.Client, s client.Scope) ([]client.VPC, error) {
			return c.ListVPCs(ctx, s)
		},
		func(v client.VPC) string { return v.Name },
		testName,
		func(ctx context.Context, c *client.Client, s client.Scope, v client.VPC) error {
			return c.DeleteVPC(ctx, s, v.ID)
		},
	)
}

func sweepDNSZones(string) error {
	zoneName := os.Getenv(envAccZoneName)
	if zoneName == "" {
		return nil
	}
	return sweep(
		func(ctx context.Context, c *client.Client, s client.Scope) ([]client.DNSZone, error) {
			return c.ListDNSZones(ctx, s)
		},
		func(z client.DNSZone) string { return z.ZoneName },
		func(name string) bool { return name == zoneName },
		func(ctx context.Context, c *client.Client, s client.Scope, z client.DNSZone) error {
			return c.DeleteDNSZone(ctx, s, z.ID)
		},
	)
}
