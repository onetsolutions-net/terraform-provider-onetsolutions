package client

import (
	"context"
	"net/http"
)

// HostingCronJob is the CronJob schema. LineKey identifies the job's line in the account's
// crontab; the create endpoint answers it empty, so it is read back from the list.
type HostingCronJob struct {
	LineKey string `json:"linekey"`
	Command string `json:"command"`
	Minute  string `json:"minute"`
	Hour    string `json:"hour"`
	Day     string `json:"day"`
	Month   string `json:"month"`
	Weekday string `json:"weekday"`
}

// HostingCronJobInput is the CreateCronJobInput schema.
type HostingCronJobInput struct {
	Command string `json:"command"`
	Minute  string `json:"minute"`
	Hour    string `json:"hour"`
	Day     string `json:"day"`
	Month   string `json:"month"`
	Weekday string `json:"weekday"`
}

// HostingSubdomain is the Subdomain schema.
type HostingSubdomain struct {
	Subdomain    string `json:"subdomain"`
	Domain       string `json:"domain"`
	FullDomain   string `json:"full_domain"`
	DocumentRoot string `json:"document_root"`
}

// HostingSubdomainInput is the CreateSubdomainInput schema.
type HostingSubdomainInput struct {
	Subdomain    string `json:"subdomain"`
	DocumentRoot string `json:"document_root,omitempty"`
}

// HostingAddonDomain is the AddonDomain schema.
type HostingAddonDomain struct {
	Domain       string `json:"domain"`
	Subdomain    string `json:"subdomain"`
	DocumentRoot string `json:"document_root"`
}

// HostingAddonDomainInput is the CreateAddonDomainInput schema.
type HostingAddonDomainInput struct {
	Domain       string `json:"domain"`
	Subdomain    string `json:"subdomain,omitempty"`
	DocumentRoot string `json:"document_root,omitempty"`
}

// HostingRedirect is the Redirect schema, also the CreateRedirectInput one.
type HostingRedirect struct {
	Domain      string `json:"domain"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Type        string `json:"type"`
}

const hostingWebPath = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}"

var (
	routeListHostingCronJobs     = newRoute(http.MethodGet, hostingWebPath+"/cron-jobs")
	routeCreateHostingCronJob    = newRoute(http.MethodPost, hostingWebPath+"/cron-jobs/create")
	routeDeleteHostingCronJob    = newRoute(http.MethodPost, hostingWebPath+"/cron-jobs/delete")
	routeListHostingSubdomains   = newRoute(http.MethodGet, hostingWebPath+"/subdomains")
	routeCreateHostingSubdomain  = newRoute(http.MethodPost, hostingWebPath+"/subdomains/create")
	routeDeleteHostingSubdomain  = newRoute(http.MethodDelete, hostingWebPath+"/subdomains/{subdomain}/delete")
	routeListHostingAddonDomains = newRoute(http.MethodGet, hostingWebPath+"/addon-domains")
	routeCreateHostingAddon      = newRoute(http.MethodPost, hostingWebPath+"/addon-domains/create")
	routeDeleteHostingAddon      = newRoute(http.MethodDelete, hostingWebPath+"/addon-domains/{domain}/delete")
	routeListHostingRedirects    = newRoute(http.MethodGet, hostingWebPath+"/redirects")
	routeCreateHostingRedirect   = newRoute(http.MethodPost, hostingWebPath+"/redirects/create")
)

// ListHostingCronJobs lists the cron jobs of a web hosting.
func (c *Client) ListHostingCronJobs(ctx context.Context, scope Scope, hostingID string) ([]HostingCronJob, error) {
	return listAll[HostingCronJob](ctx, c, routeListHostingCronJobs, scope.Params("id", hostingID), nil)
}

// CreateHostingCronJob adds a cron job to a web hosting.
func (c *Client) CreateHostingCronJob(ctx context.Context, scope Scope, hostingID string, in HostingCronJobInput) (*HostingCronJob, error) {
	var out HostingCronJob
	if err := c.do(ctx, routeCreateHostingCronJob, scope.Params("id", hostingID), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteHostingCronJob removes the cron job of a web hosting at lineKey.
func (c *Client) DeleteHostingCronJob(ctx context.Context, scope Scope, hostingID, lineKey string) error {
	return c.do(ctx, routeDeleteHostingCronJob, scope.Params("id", hostingID), map[string]string{"linekey": lineKey}, nil)
}

// ListHostingSubdomains lists the subdomains of a web hosting.
func (c *Client) ListHostingSubdomains(ctx context.Context, scope Scope, hostingID string) ([]HostingSubdomain, error) {
	return listAll[HostingSubdomain](ctx, c, routeListHostingSubdomains, scope.Params("id", hostingID), nil)
}

// CreateHostingSubdomain adds a subdomain of the main domain to a web hosting.
func (c *Client) CreateHostingSubdomain(ctx context.Context, scope Scope, hostingID string, in HostingSubdomainInput) (*HostingSubdomain, error) {
	var out HostingSubdomain
	if err := c.do(ctx, routeCreateHostingSubdomain, scope.Params("id", hostingID), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteHostingSubdomain removes a subdomain, named by its label or its full domain.
func (c *Client) DeleteHostingSubdomain(ctx context.Context, scope Scope, hostingID, subdomain string) error {
	return c.do(ctx, routeDeleteHostingSubdomain, scope.Params("id", hostingID, "subdomain", subdomain), nil, nil)
}

// ListHostingAddonDomains lists the addon domains of a web hosting.
func (c *Client) ListHostingAddonDomains(ctx context.Context, scope Scope, hostingID string) ([]HostingAddonDomain, error) {
	return listAll[HostingAddonDomain](ctx, c, routeListHostingAddonDomains, scope.Params("id", hostingID), nil)
}

// CreateHostingAddonDomain adds an addon domain to a web hosting.
func (c *Client) CreateHostingAddonDomain(ctx context.Context, scope Scope, hostingID string, in HostingAddonDomainInput) (*HostingAddonDomain, error) {
	var out HostingAddonDomain
	if err := c.do(ctx, routeCreateHostingAddon, scope.Params("id", hostingID), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteHostingAddonDomain removes an addon domain from a web hosting.
func (c *Client) DeleteHostingAddonDomain(ctx context.Context, scope Scope, hostingID, domain string) error {
	return c.do(ctx, routeDeleteHostingAddon, scope.Params("id", hostingID, "domain", domain), nil, nil)
}

// ListHostingRedirects lists the redirects of a web hosting.
func (c *Client) ListHostingRedirects(ctx context.Context, scope Scope, hostingID string) ([]HostingRedirect, error) {
	return listAll[HostingRedirect](ctx, c, routeListHostingRedirects, scope.Params("id", hostingID), nil)
}

// CreateHostingRedirect adds a redirect to a web hosting. A redirect cannot be removed afterwards.
func (c *Client) CreateHostingRedirect(ctx context.Context, scope Scope, hostingID string, in HostingRedirect) (*HostingRedirect, error) {
	var out HostingRedirect
	if err := c.do(ctx, routeCreateHostingRedirect, scope.Params("id", hostingID), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
