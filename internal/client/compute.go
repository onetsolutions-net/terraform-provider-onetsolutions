package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Instance statuses (the billing status of the service).
const (
	InstanceStatusPending      = "pending"
	InstanceStatusProvisioning = "provisioning"
	InstanceStatusActive       = "active"
	InstanceStatusSuspended    = "suspended"
	InstanceStatusTerminated   = "terminated"
	InstanceStatusCancelled    = "cancelled"
	InstanceStatusError        = "error"
)

// Codes of the 409 the power actions answer when the instance already is in the requested state,
// as the API's own power status (not exposed) reads it.
const (
	CodeInstanceAlreadyRunning = "instance_already_running"
	CodeInstanceAlreadyStopped = "instance_already_stopped"
)

// Power states reported by state_sync.vm_status.
const (
	VMStatusRunning = "running"
	VMStatusStopped = "stopped"
	VMStatusShutoff = "shutoff"
)

// Task types the provider waits for after an instance update.
const (
	TaskTypeChangeHostname = "change_hostname"
	TaskTypeUpdateVPC      = "update_vpc"
)

// InstanceSizeRef is the part of the InstanceSize schema an instance needs.
type InstanceSizeRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	IsActive bool   `json:"is_active"`
}

// InstanceImageRef is the part of the InstanceImage schema an instance needs.
type InstanceImageRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	OSName    string `json:"os_name"`
	OSVersion string `json:"os_version"`
}

// InstanceVPCRef is the part of the VPC schema an instance carries.
type InstanceVPCRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// InstanceAddress is the IPAddress schema, as nested in an instance.
type InstanceAddress struct {
	ID         string `json:"id"`
	Address    string `json:"address"`
	IPType     string `json:"ip_type"`
	Interface  int64  `json:"interface"`
	IsPrimary  bool   `json:"is_primary"`
	ReverseDNS string `json:"reverse_dns"`
}

// InstanceVolume is the VolumeSummary schema.
type InstanceVolume struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SizeGB   int64  `json:"size_gb"`
	Status   string `json:"status"`
	DiskType string `json:"disk_type"`
}

// InstanceStateSync is the InstanceStateSync schema: the last power state read.
type InstanceStateSync struct {
	VMStatus     string  `json:"vm_status"`
	LastSyncedAt *string `json:"last_synced_at"`
}

// CancellationReasonCodes are the reason codes delete_instance and cancel_instance accept.
var CancellationReasonCodes = []string{
	"too_expensive", "no_longer_needed", "missing_feature", "technical_issue", "switching_provider", "other",
}

// DefaultCancellationReasonCode is the reason_code delete_instance applies when none is sent.
const DefaultCancellationReasonCode = "no_longer_needed"

// CancellationReasonMinWords is the API's minimum length of a cancellation reason, in words.
const CancellationReasonMinWords = 7

// CancellationReasonMaxLength is the API's maximum length of a cancellation reason, in characters.
const CancellationReasonMaxLength = 1000

// OptionalField is a nullable JSON field that also records whether the response carried it at
// all: an API older than the field omits it, which a plain pointer cannot tell from null.
type OptionalField struct {
	Present bool
	Value   *string
}

// UnmarshalJSON runs only when the key is in the response.
func (f *OptionalField) UnmarshalJSON(raw []byte) error {
	f.Present = true
	return json.Unmarshal(raw, &f.Value)
}

// Instance is the Instance schema.
type Instance struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	// OrderID is the order the instance was created from: the order's local_order_id. An older API
	// version does not serve it.
	OrderID         OptionalField     `json:"order_id"`
	Name            string            `json:"name"`
	Status          string            `json:"status"`
	Size            *InstanceSizeRef  `json:"size"`
	VPC             *InstanceVPCRef   `json:"vpc"`
	Image           *InstanceImageRef `json:"image"`
	OSName          string            `json:"os_name"`
	PasswordReset   bool              `json:"password_reset_enabled"`
	PrimaryIPv4     *string           `json:"primary_ipv4"`
	PrimaryIPv6     *string           `json:"primary_ipv6"`
	IPAddresses     []InstanceAddress `json:"ip_addresses"`
	Volumes         []InstanceVolume  `json:"volumes"`
	BillingCycle    string            `json:"billing_cycle"`
	RecurringAmount *string           `json:"recurring_amount"`
	NextBillingDate *string           `json:"next_billing_date"`
	// CancellationRequestedAt is set once a cancellation at the end of the billing period is
	// requested; BillingEndDate is then the day the instance stops running and being billed.
	CancellationRequestedAt *string            `json:"cancellation_requested_at"`
	BillingEndDate          *string            `json:"billing_end_date"`
	Tags                    []string           `json:"tags"`
	IsActive                bool               `json:"is_active"`
	IsInstalled             bool               `json:"is_installed"`
	StateSync               *InstanceStateSync `json:"state_sync"`
	Locked                  bool               `json:"locked"`
	LockReason              *string            `json:"lock_reason"`
	Created                 string             `json:"created"`

	// BackupEnabled reports whether the instance has the backup option, a paid add-on.
	BackupEnabled bool `json:"backup_enabled"`
}

// VMStatus returns the last power state read, empty when unknown.
func (i *Instance) VMStatus() string {
	if i.StateSync == nil {
		return ""
	}
	return i.StateSync.VMStatus
}

// Gone reports whether the instance no longer exists for its owner.
func (i *Instance) Gone() bool {
	return i.Status == InstanceStatusTerminated || i.Status == InstanceStatusCancelled
}

// OneClickAppSelection is the OneClickAppSelection schema.
type OneClickAppSelection struct {
	Slug       string            `json:"slug"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// InstanceCreate is the body of create_instance and of preview_instance_order.
type InstanceCreate struct {
	Name                 string                 `json:"name"`
	SizeID               string                 `json:"size_id"`
	ImageID              string                 `json:"image_id"`
	BillingCycle         string                 `json:"billing_cycle"`
	SSHKeys              []string               `json:"ssh_keys,omitempty"`
	VPCID                string                 `json:"vpc_id,omitempty"`
	VolumeID             string                 `json:"volume_id,omitempty"`
	Tags                 []string               `json:"tags,omitempty"`
	OneClickApps         []OneClickAppSelection `json:"one_click_apps,omitempty"`
	ControlPanel         string                 `json:"control_panel,omitempty"`
	AddonIDs             []string               `json:"addon_ids,omitempty"`
	AcceptedContractIDs  []string               `json:"accepted_contract_ids,omitempty"`
	InstallManagementKey bool                   `json:"install_management_key"`
	AllowPasswordReset   bool                   `json:"allow_password_reset"`
}

// InstanceOrder is the answer of create_instance (InstanceOrderResponse). The API places an
// order and answers with the order, not the instance, which is created once the order is paid:
// AmountDue > 0 means the account balance did not cover it, and the invoice awaits payment.
// LocalOrderID is the value of the instance's order_id once it exists; OrderID is the order's
// number with the billing department, which no list filters on.
type InstanceOrder struct {
	OrderID           *int64   `json:"order_id"`
	OrderNumber       string   `json:"order_number"`
	InvoiceID         *int64   `json:"invoice_id"`
	LocalOrderID      *string  `json:"local_order_id"`
	Total             *float64 `json:"total"`
	AmountDue         *float64 `json:"amount_due"`
	CreditApplied     *float64 `json:"credit_applied"`
	Status            string   `json:"status"`
	InvoicePaymentURL *string  `json:"invoice_payment_url"`
}

// AwaitsPayment reports whether the order is placed but unpaid: no instance exists until it is.
func (o *InstanceOrder) AwaitsPayment() bool {
	return o.AmountDue != nil && *o.AmountDue > 0
}

// InstanceOrderPreview is the answer of preview_instance_order.
type InstanceOrderPreview struct {
	Total        *float64 `json:"total"`
	TotalInclTax *float64 `json:"total_incl_tax"`
	SetupTotal   *float64 `json:"setup_total"`
	AmountDue    *float64 `json:"amount_due"`
	Currency     string   `json:"currency"`
}

// InstanceUpdate is the body of partial_update_instance; nil fields are left unchanged.
// DetachVPC sends "vpc_id": null.
type InstanceUpdate struct {
	Name      *string
	Tags      *[]string
	VPCID     *string
	DetachVPC bool
}

// MarshalJSON writes only the fields to change, with an explicit null to detach the VPC.
func (u InstanceUpdate) MarshalJSON() ([]byte, error) {
	body := map[string]any{}
	if u.Name != nil {
		body["name"] = *u.Name
	}
	if u.Tags != nil {
		body["tags"] = *u.Tags
	}
	switch {
	case u.DetachVPC:
		body["vpc_id"] = nil
	case u.VPCID != nil:
		body["vpc_id"] = *u.VPCID
	}
	return json.Marshal(body)
}

// InstanceDelete is the optional body of delete_instance. An empty Reason is left out, so the
// API's default sentence applies.
type InstanceDelete struct {
	ReasonCode string `json:"reason_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// InstanceResize is the body of resize_instance and preview_instance_resize.
type InstanceResize struct {
	SizeID       string `json:"size_id"`
	BillingCycle string `json:"billing_cycle,omitempty"`
}

// InstanceResizeOrder is the answer of resize_instance: an upgrade order, applied once paid.
type InstanceResizeOrder struct {
	OrderID    *int64  `json:"order_id"`
	InvoiceID  *int64  `json:"invoice_id"`
	InvoiceURL *string `json:"invoice_url"`
	Message    string  `json:"message"`
}

// InstanceResizePreview is the InstanceResizePreview schema.
type InstanceResizePreview struct {
	TotalPrice     *string `json:"total_price"`
	RecurringPrice *string `json:"recurring_price"`
	Currency       string  `json:"currency"`
	BillingCycle   string  `json:"billing_cycle"`
}

// InstanceTaskRef is the answer of the power actions.
type InstanceTaskRef struct {
	TaskID string `json:"task_id"`
}

const computeInstancesPath = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances"

var (
	routeListComputeInstances  = newRoute(http.MethodGet, computeInstancesPath)
	routeCreateComputeInstance = newRoute(http.MethodPost, computeInstancesPath)
	routePreviewInstanceOrder  = newRetrySafeRoute(computeInstancesPath + "/order-preview")
	routeGetComputeInstance    = newRoute(http.MethodGet, computeInstancesPath+"/{id}")
	routeUpdateComputeInstance = newRoute(http.MethodPatch, computeInstancesPath+"/{id}")
	routeDeleteComputeInstance = newRoute(http.MethodDelete, computeInstancesPath+"/{id}")
	routeStartComputeInstance  = newRoute(http.MethodPost, computeInstancesPath+"/{id}/start")
	routeStopComputeInstance   = newRoute(http.MethodPost, computeInstancesPath+"/{id}/stop")
	routeResizeComputeInstance = newRoute(http.MethodPost, computeInstancesPath+"/{id}/resize")
	routePreviewInstanceResize = newRetrySafeRoute(computeInstancesPath + "/{id}/resize-preview")
	routeEligibleInstanceSizes = newRoute(http.MethodGet, computeInstancesPath+"/{id}/eligible-sizes")
	routeEnableInstanceIPv6    = newRoute(http.MethodPost, computeInstancesPath+"/{id}/ipv6")
)

// CodeIPv6AlreadyEnabled is the code of the 409 enable_instance_ipv6 answers when the instance
// already has an IPv6 block.
const CodeIPv6AlreadyEnabled = "ipv6_already_enabled"

// ListInstancesByOrder lists the instances the API reports as created from an order, whatever
// their status. An older API version ignores the filter and lists every instance without
// order_id: the caller must check each instance's OrderID itself.
func (c *Client) ListInstancesByOrder(ctx context.Context, scope Scope, localOrderID string) ([]Instance, error) {
	return listAll[Instance](ctx, c, routeListComputeInstances, scope.Params(),
		url.Values{"is_active": {"all"}, "order_id": {localOrderID}})
}

// ListActiveInstances lists the instances of a project that are neither cancelled nor terminated.
func (c *Client) ListActiveInstances(ctx context.Context, scope Scope) ([]Instance, error) {
	return listAll[Instance](ctx, c, routeListComputeInstances, scope.Params(), nil)
}

// CreateInstance orders an instance. The order is billed: it is never retried blindly.
func (c *Client) CreateInstance(ctx context.Context, scope Scope, in InstanceCreate) (*InstanceOrder, error) {
	var order InstanceOrder
	if err := c.do(ctx, routeCreateComputeInstance, scope.Params(), in, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// PreviewInstanceOrder prices an instance order without placing it.
func (c *Client) PreviewInstanceOrder(ctx context.Context, scope Scope, in InstanceCreate) (*InstanceOrderPreview, error) {
	var preview InstanceOrderPreview
	if err := c.do(ctx, routePreviewInstanceOrder, scope.Params(), in, &preview); err != nil {
		return nil, err
	}
	return &preview, nil
}

// GetInstance reads one instance.
func (c *Client) GetInstance(ctx context.Context, scope Scope, id string) (*Instance, error) {
	var instance Instance
	if err := c.do(ctx, routeGetComputeInstance, scope.Params("id", id), nil, &instance); err != nil {
		return nil, err
	}
	return &instance, nil
}

// UpdateInstance changes the name, tags or VPC of an instance. A new name or VPC queues a task.
func (c *Client) UpdateInstance(ctx context.Context, scope Scope, id string, in InstanceUpdate) (*Instance, error) {
	var instance Instance
	if err := c.do(ctx, routeUpdateComputeInstance, scope.Params("id", id), in, &instance); err != nil {
		return nil, err
	}
	return &instance, nil
}

// DeleteInstance requests the cancellation of an instance at the end of its billing period and
// returns the instance: it keeps running and billing until BillingEndDate. It is idempotent: an
// instance whose cancellation is already requested, or that has ended, answers the same way.
func (c *Client) DeleteInstance(ctx context.Context, scope Scope, id string, in InstanceDelete) (*Instance, error) {
	var instance Instance
	if err := c.do(ctx, routeDeleteComputeInstance, scope.Params("id", id), in, &instance); err != nil {
		return nil, err
	}
	return &instance, nil
}

// StartInstance powers an instance on and returns the task doing it.
func (c *Client) StartInstance(ctx context.Context, scope Scope, id string) (string, error) {
	return c.powerAction(ctx, routeStartComputeInstance, scope, id)
}

// StopInstance shuts an instance down gracefully and returns the task doing it.
func (c *Client) StopInstance(ctx context.Context, scope Scope, id string) (string, error) {
	return c.powerAction(ctx, routeStopComputeInstance, scope, id)
}

func (c *Client) powerAction(ctx context.Context, r Route, scope Scope, id string) (string, error) {
	var ref InstanceTaskRef
	if err := c.do(ctx, r, scope.Params("id", id), nil, &ref); err != nil {
		return "", err
	}
	return ref.TaskID, nil
}

// ResizeInstance orders a new size and/or billing cycle; the change applies once the order is paid.
func (c *Client) ResizeInstance(ctx context.Context, scope Scope, id string, in InstanceResize) (*InstanceResizeOrder, error) {
	var order InstanceResizeOrder
	if err := c.do(ctx, routeResizeComputeInstance, scope.Params("id", id), in, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// PreviewInstanceResize prices a resize without ordering it.
func (c *Client) PreviewInstanceResize(ctx context.Context, scope Scope, id string, in InstanceResize) (*InstanceResizePreview, error) {
	var preview InstanceResizePreview
	if err := c.do(ctx, routePreviewInstanceResize, scope.Params("id", id), in, &preview); err != nil {
		return nil, err
	}
	return &preview, nil
}

// ListEligibleInstanceSizes lists the sizes an instance may be resized to.
func (c *Client) ListEligibleInstanceSizes(ctx context.Context, scope Scope, id string) ([]InstanceSizeRef, error) {
	return listAll[InstanceSizeRef](ctx, c, routeEligibleInstanceSizes, scope.Params("id", id), nil)
}

// EnableInstanceIPv6 reserves an IPv6 block for an instance and returns the task configuring it.
// IPv6 cannot be disabled again.
func (c *Client) EnableInstanceIPv6(ctx context.Context, scope Scope, id string) (string, error) {
	return c.powerAction(ctx, routeEnableInstanceIPv6, scope, id)
}
