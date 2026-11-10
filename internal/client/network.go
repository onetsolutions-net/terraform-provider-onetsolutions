package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// Task types of the network area's asynchronous operations.
const (
	TaskTypeApplyFirewall    = "apply_firewall"
	TaskTypeCreateSnapshot   = "create_snapshot"
	TaskTypeDeleteSnapshot   = "delete_snapshot"
	TaskTypeUpdateReverseDNS = "update_reverse_dns"
)

// CodeInstanceTaskInProgress is the code of the 409 the API answers while a task holds the
// instance; CodeInstanceLocked the one it answers while the instance is locked.
const (
	CodeInstanceTaskInProgress = "instance_task_in_progress"
	CodeInstanceLocked         = "instance_locked"
)

// FirewallRule is the InstanceFirewallRule schema. The API stores port ranges as
// destination_port and serves a non-null source_label for well-known platform sources.
type FirewallRule struct {
	ID              string  `json:"id"`
	Instance        string  `json:"instance"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Direction       string  `json:"direction"`
	Action          string  `json:"action"`
	Protocol        string  `json:"protocol"`
	DestinationPort string  `json:"destination_port"`
	SourceCIDR      string  `json:"source_cidr"`
	SourceLabel     *string `json:"source_label"`
	DestinationCIDR string  `json:"destination_cidr"`
	Priority        int64   `json:"priority"`
	IsEnabled       bool    `json:"is_enabled"`
	Created         string  `json:"created"`
	Modified        string  `json:"modified"`
}

// FirewallRuleCreate is the body of create_firewall_rule (InstanceFirewallRuleCreate). The API
// names the source "source" here and creates every rule enabled, without destination CIDR:
// those are set by a PATCH afterwards.
type FirewallRuleCreate struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	Direction       string `json:"direction"`
	Action          string `json:"action"`
	Protocol        string `json:"protocol"`
	Source          string `json:"source"`
	DestinationPort string `json:"destination_port,omitempty"`
	Priority        int64  `json:"priority"`
}

// FirewallRuleUpdate is the body of partial_update_firewall_rule; nil fields are left unchanged.
type FirewallRuleUpdate struct {
	Name            *string `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	Direction       *string `json:"direction,omitempty"`
	Action          *string `json:"action,omitempty"`
	Protocol        *string `json:"protocol,omitempty"`
	DestinationPort *string `json:"destination_port,omitempty"`
	SourceCIDR      *string `json:"source_cidr,omitempty"`
	DestinationCIDR *string `json:"destination_cidr,omitempty"`
	Priority        *int64  `json:"priority,omitempty"`
	IsEnabled       *bool   `json:"is_enabled,omitempty"`
}

// Snapshot is the InstanceSnapshot schema. size_gb is a decimal the API serves as a string, null
// until the snapshot exists on the platform side.
type Snapshot struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	SizeGB      *string `json:"size_gb"`
	Created     string  `json:"created"`
	Modified    string  `json:"modified"`
	// TaskID is only set on the answer of create_snapshot.
	TaskID string `json:"task_id,omitempty"`
}

// SnapshotCreate is the body of create_snapshot.
type SnapshotCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Snapshot statuses.
const (
	SnapshotPending  = "pending"
	SnapshotPresent  = "present"
	SnapshotDeleting = "deleting"
	SnapshotFailed   = "failed"
)

// InstanceIPAddress is the IPAddress schema, an address assigned to an instance.
type InstanceIPAddress struct {
	ID           string  `json:"id"`
	Address      string  `json:"address"`
	IPType       string  `json:"ip_type"`
	Interface    int     `json:"interface"`
	Netmask      *string `json:"netmask"`
	Gateway      *string `json:"gateway"`
	NetworkRange *string `json:"network_range"`
	MACAddress   string  `json:"mac_address"`
	IsPrimary    bool    `json:"is_primary"`
	ReverseDNS   string  `json:"reverse_dns"`
	Created      string  `json:"created"`
	Modified     string  `json:"modified"`
}

type taskReference struct {
	TaskID string `json:"task_id"`
}

type parameterizedTask struct {
	ID         string          `json:"id"`
	TaskType   string          `json:"task_type"`
	Status     string          `json:"status"`
	Parameters json.RawMessage `json:"parameters"`
}

const (
	firewallPath    = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/firewall"
	snapshotsPath   = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/snapshots"
	ipAddressesPath = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/ip-addresses"
)

var (
	routeListFirewallRules  = newRoute(http.MethodGet, firewallPath)
	routeCreateFirewallRule = newRoute(http.MethodPost, firewallPath)
	routeUpdateFirewallRule = newRoute(http.MethodPatch, firewallPath+"/{id}")
	routeDeleteFirewallRule = newRoute(http.MethodDelete, firewallPath+"/{id}")
	// Applying the rule set again has no effect beyond the first application.
	routeApplyFirewall = newRetrySafeRoute(firewallPath + "/apply")

	routeCreateSnapshot = newRoute(http.MethodPost, snapshotsPath)
	routeGetSnapshot    = newRoute(http.MethodGet, snapshotsPath+"/{id}")
	routeDeleteSnapshot = newRoute(http.MethodDelete, snapshotsPath+"/{id}")

	routeListInstanceIPAddresses = newRoute(http.MethodGet, ipAddressesPath)
	routeGetInstanceIPAddress    = newRoute(http.MethodGet, ipAddressesPath+"/{id}")
	routeUpdateInstanceIPAddress = newRoute(http.MethodPatch, ipAddressesPath+"/{id}")
)

// ListFirewallRules lists every firewall rule of an instance, platform-managed ones included,
// in priority order.
func (c *Client) ListFirewallRules(ctx context.Context, scope Scope, instanceID string) ([]FirewallRule, error) {
	return listAll[FirewallRule](ctx, c, routeListFirewallRules, scope.Params("instance_id", instanceID), nil)
}

// CreateFirewallRule adds a rule to an instance. The API queues a firewall synchronization
// itself, so the instance is busy (409) for a moment afterwards.
func (c *Client) CreateFirewallRule(ctx context.Context, scope Scope, instanceID string, body FirewallRuleCreate) (*FirewallRule, error) {
	var out FirewallRule
	if err := c.do(ctx, routeCreateFirewallRule, scope.Params("instance_id", instanceID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateFirewallRule changes a rule of an instance.
func (c *Client) UpdateFirewallRule(ctx context.Context, scope Scope, instanceID, id string, body FirewallRuleUpdate) (*FirewallRule, error) {
	var out FirewallRule
	if err := c.do(ctx, routeUpdateFirewallRule, scope.Params("instance_id", instanceID, "id", id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteFirewallRule removes a rule of an instance.
func (c *Client) DeleteFirewallRule(ctx context.Context, scope Scope, instanceID, id string) error {
	return c.do(ctx, routeDeleteFirewallRule, scope.Params("instance_id", instanceID, "id", id), nil, nil)
}

// ApplyFirewall queues the apply_firewall task of an instance and returns its ID.
func (c *Client) ApplyFirewall(ctx context.Context, scope Scope, instanceID string) (string, error) {
	var out taskReference
	if err := c.do(ctx, routeApplyFirewall, scope.Params("instance_id", instanceID), map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.TaskID, nil
}

// CreateSnapshot queues the snapshot of an instance; the answer carries the create_snapshot
// task in TaskID.
func (c *Client) CreateSnapshot(ctx context.Context, scope Scope, instanceID string, body SnapshotCreate) (*Snapshot, error) {
	var out Snapshot
	if err := c.do(ctx, routeCreateSnapshot, scope.Params("instance_id", instanceID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSnapshot reads one snapshot of an instance; a deleted snapshot is a 404.
func (c *Client) GetSnapshot(ctx context.Context, scope Scope, instanceID, id string) (*Snapshot, error) {
	var out Snapshot
	if err := c.do(ctx, routeGetSnapshot, scope.Params("instance_id", instanceID, "id", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSnapshot queues the deletion of a snapshot and returns the delete_snapshot task ID.
func (c *Client) DeleteSnapshot(ctx context.Context, scope Scope, instanceID, id string) (string, error) {
	var out taskReference
	if err := c.do(ctx, routeDeleteSnapshot, scope.Params("instance_id", instanceID, "id", id), nil, &out); err != nil {
		return "", err
	}
	return out.TaskID, nil
}

// ListInstanceIPAddresses lists the addresses of an instance, primary first.
func (c *Client) ListInstanceIPAddresses(ctx context.Context, scope Scope, instanceID string) ([]InstanceIPAddress, error) {
	return listAll[InstanceIPAddress](ctx, c, routeListInstanceIPAddresses, scope.Params("instance_id", instanceID), nil)
}

// GetInstanceIPAddress reads one address of an instance.
func (c *Client) GetInstanceIPAddress(ctx context.Context, scope Scope, instanceID, id string) (*InstanceIPAddress, error) {
	var out InstanceIPAddress
	if err := c.do(ctx, routeGetInstanceIPAddress, scope.Params("instance_id", instanceID, "id", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateReverseDNS requests a new reverse DNS hostname for an address. The API answers with the
// address as it is and queues an update_reverse_dns task without returning its ID (none when
// the hostname is already set); FindReverseDNSTask finds it.
func (c *Client) UpdateReverseDNS(ctx context.Context, scope Scope, instanceID, id, hostname string) (*InstanceIPAddress, error) {
	var out InstanceIPAddress
	body := map[string]string{"reverse_dns": hostname}
	if err := c.do(ctx, routeUpdateInstanceIPAddress, scope.Params("instance_id", instanceID, "id", id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindReverseDNSTask returns the ID of the most recent update_reverse_dns task of an instance
// that sets hostname on the address ipAddressID, empty when there is none. It reads the recent
// tasks only (see ListRecentInstanceTasks), where the task just queued is.
func (c *Client) FindReverseDNSTask(ctx context.Context, scope Scope, instanceID, ipAddressID, hostname string) (string, error) {
	tasks, err := listFirstPage[parameterizedTask](ctx, c, routeListInstanceTasks, scope.Params("instance_id", instanceID))
	if err != nil {
		return "", err
	}
	for _, task := range tasks {
		if task.TaskType != TaskTypeUpdateReverseDNS {
			continue
		}
		var parameters struct {
			IPAddressID string `json:"ip_address_id"`
			ReverseDNS  string `json:"reverse_dns"`
		}
		if json.Unmarshal(task.Parameters, &parameters) != nil {
			continue
		}
		if parameters.IPAddressID == ipAddressID && parameters.ReverseDNS == hostname {
			return task.ID, nil
		}
	}
	return "", nil
}
