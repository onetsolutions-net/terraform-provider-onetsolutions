package client

import (
	"context"
	"net/http"
)

// Web hosting statuses.
const (
	WebHostingStatusPending    = "pending"
	WebHostingStatusActive     = "active"
	WebHostingStatusSuspended  = "suspended"
	WebHostingStatusTerminated = "terminated"
	WebHostingStatusCancelled  = "cancelled"
	WebHostingStatusFailed     = "failed"
)

// Error codes of the web hosting cancellation.
const (
	CodeHostingNotCancellable        = "hosting_not_cancellable"
	CodeHostingCancellationRequested = "hosting_cancellation_already_requested"
	CodeHostingNotLinkedToBilling    = "hosting_not_linked_to_billing"
	CodeHostingBillingUnavailable    = "hosting_billing_unavailable"
)

// HostingCancellationDefaultReason is the reason sent with a cancellation when none is
// configured: the API requires a sentence of at least CancellationReasonMinWords words.
const HostingCancellationDefaultReason = "Cancelled with Terraform: the web hosting was removed from the configuration."

// Gone reports whether the web hosting has ended: terminated or cancelled.
func (h *WebHosting) Gone() bool {
	return h.Status == WebHostingStatusTerminated || h.Status == WebHostingStatusCancelled
}

// HostingOrderCreate is the HostingOrderRequest schema.
type HostingOrderCreate struct {
	PlanID       string `json:"plan_id"`
	DomainSource string `json:"domain_source"`
	DomainName   string `json:"domain_name,omitempty"`
	EPPCode      string `json:"epp_code,omitempty"`
	BillingCycle string `json:"billing_cycle,omitempty"`
}

// HostingOrder is the HostingOrderResponse schema: the order placed and the web hosting it
// created, pending until the order is paid and the account provisioned.
type HostingOrder struct {
	OrderID           *int64   `json:"order_id"`
	OrderNumber       *string  `json:"order_number"`
	InvoiceID         *int64   `json:"invoice_id"`
	LocalOrderID      *string  `json:"local_order_id"`
	Total             *float64 `json:"total"`
	AmountDue         *float64 `json:"amount_due"`
	CreditApplied     *float64 `json:"credit_applied"`
	Status            string   `json:"status"`
	InvoicePaymentURL *string  `json:"invoice_payment_url"`
	HostingID         string   `json:"hosting_id"`
	HostingStatus     string   `json:"hosting_status"`
	DomainID          *string  `json:"domain_id"`
	DomainName        string   `json:"domain_name"`
}

// AwaitsPayment reports whether the order is placed but unpaid: the web hosting stays pending
// until it is.
func (o *HostingOrder) AwaitsPayment() bool {
	return o.AmountDue != nil && *o.AmountDue > 0
}

// HostingPlanChange is the HostingChangePlan schema.
type HostingPlanChange struct {
	PlanID       string `json:"plan_id"`
	BillingCycle string `json:"billing_cycle,omitempty"`
}

// HostingPlanChangeOrder is the HostingChangePlanResponse schema.
type HostingPlanChangeOrder struct {
	OrderID    *int64  `json:"order_id"`
	InvoiceID  *int64  `json:"invoice_id"`
	InvoiceURL *string `json:"invoice_url"`
	Message    string  `json:"message"`
}

// HostingCancellation is the CancellationRequest schema.
type HostingCancellation struct {
	ReasonCode string `json:"reason_code,omitempty"`
	Reason     string `json:"reason"`
}

// PHPVersion is a PHP version installed on the server of a web hosting.
type PHPVersion struct {
	Version string `json:"version"`
	Display string `json:"display"`
}

// DomainPHPVersion is the PHP version a domain of a web hosting runs.
type DomainPHPVersion struct {
	Domain     string `json:"domain"`
	PHPVersion string `json:"php_version"`
}

// PHPVersionInherit is the version of a domain that runs the account's default PHP version.
const PHPVersionInherit = "inherit"

// SetPHPVersion is the SetPHPVersionInput schema.
type SetPHPVersion struct {
	PHPVersion string `json:"php_version"`
	Domain     string `json:"domain,omitempty"`
}

// WordPressSite is the WordPressSite schema.
type WordPressSite struct {
	ID      string `json:"id"`
	Domain  string `json:"domain"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// WordPressInstall is the InstallWordPressInput schema.
type WordPressInstall struct {
	Domain        string `json:"domain"`
	Path          string `json:"path"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`
	AdminEmail    string `json:"admin_email"`
	Title         string `json:"title,omitempty"`
	Language      string `json:"language,omitempty"`
}

const webHostingPath = webHostingsPath + "/{id}"

var (
	routeOrderWebHosting       = newRoute(http.MethodPost, webHostingsPath+"/order")
	routeCancelWebHosting      = newRoute(http.MethodPost, webHostingPath+"/cancel")
	routeChangeHostingPlan     = newRoute(http.MethodPost, webHostingPath+"/change-plan")
	routeListEligibleHosting   = newRoute(http.MethodGet, webHostingPath+"/eligible-plans")
	routeListPHPVersions       = newRoute(http.MethodGet, webHostingPath+"/php-versions")
	routeListDomainPHPVersions = newRoute(http.MethodGet, webHostingPath+"/domains-php-versions")
	routeSetPHPVersion         = newRoute(http.MethodPost, webHostingPath+"/php-version")
	routeListWordPressSites    = newRoute(http.MethodGet, webHostingPath+"/wordpress")
	routeInstallWordPress      = newRoute(http.MethodPost, webHostingPath+"/wordpress/install")
	routeUpdateWordPress       = newRoute(http.MethodPost, webHostingPath+"/wordpress/{instance_id}/update")
	routeDeleteWordPress       = newRoute(http.MethodPost, webHostingPath+"/wordpress/{instance_id}/delete")
)

// OrderWebHosting places a billed order for a web hosting. It is never retried after it was
// sent: a lost answer may hide a placed order.
func (c *Client) OrderWebHosting(ctx context.Context, scope Scope, in HostingOrderCreate) (*HostingOrder, error) {
	var order HostingOrder
	if err := c.do(ctx, routeOrderWebHosting, scope.Params(), in, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// CancelWebHosting requests the cancellation of a web hosting at the end of its billing period.
func (c *Client) CancelWebHosting(ctx context.Context, scope Scope, id string, in HostingCancellation) (*WebHosting, error) {
	var hosting WebHosting
	if err := c.do(ctx, routeCancelWebHosting, scope.Params("id", id), in, &hosting); err != nil {
		return nil, err
	}
	return &hosting, nil
}

// ChangeWebHostingPlan orders a change of plan, applied once the order is paid.
func (c *Client) ChangeWebHostingPlan(ctx context.Context, scope Scope, id string, in HostingPlanChange) (*HostingPlanChangeOrder, error) {
	var order HostingPlanChangeOrder
	if err := c.do(ctx, routeChangeHostingPlan, scope.Params("id", id), in, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

// ListEligibleHostingPlans lists the plans a web hosting can change to.
func (c *Client) ListEligibleHostingPlans(ctx context.Context, scope Scope, id string) ([]HostingPlan, error) {
	return listAll[HostingPlan](ctx, c, routeListEligibleHosting, scope.Params("id", id), nil)
}

// ListHostingPHPVersions lists the PHP versions installed on the server of a web hosting.
func (c *Client) ListHostingPHPVersions(ctx context.Context, scope Scope, id string) ([]PHPVersion, error) {
	return listAll[PHPVersion](ctx, c, routeListPHPVersions, scope.Params("id", id), nil)
}

// ListHostingDomainPHPVersions lists the PHP version of each domain of a web hosting.
func (c *Client) ListHostingDomainPHPVersions(ctx context.Context, scope Scope, id string) ([]DomainPHPVersion, error) {
	return listAll[DomainPHPVersion](ctx, c, routeListDomainPHPVersions, scope.Params("id", id), nil)
}

// SetHostingPHPVersion sets the PHP version of a domain of a web hosting.
func (c *Client) SetHostingPHPVersion(ctx context.Context, scope Scope, id string, in SetPHPVersion) error {
	return c.do(ctx, routeSetPHPVersion, scope.Params("id", id), in, nil)
}

// ListHostingWordPressSites lists the WordPress sites of a web hosting.
func (c *Client) ListHostingWordPressSites(ctx context.Context, scope Scope, id string) ([]WordPressSite, error) {
	return listAll[WordPressSite](ctx, c, routeListWordPressSites, scope.Params("id", id), nil)
}

// InstallHostingWordPress installs a WordPress site on a web hosting.
func (c *Client) InstallHostingWordPress(ctx context.Context, scope Scope, id string, in WordPressInstall) (*WordPressSite, error) {
	var site WordPressSite
	if err := c.do(ctx, routeInstallWordPress, scope.Params("id", id), in, &site); err != nil {
		return nil, err
	}
	return &site, nil
}

// UpdateHostingWordPress updates a WordPress site to the latest version available.
func (c *Client) UpdateHostingWordPress(ctx context.Context, scope Scope, id, siteID string) error {
	return c.do(ctx, routeUpdateWordPress, scope.Params("id", id, "instance_id", siteID), struct{}{}, nil)
}

// DeleteHostingWordPress deletes a WordPress site with its files and database.
func (c *Client) DeleteHostingWordPress(ctx context.Context, scope Scope, id, siteID string) error {
	return c.do(ctx, routeDeleteWordPress, scope.Params("id", id, "instance_id", siteID), struct{}{}, nil)
}
