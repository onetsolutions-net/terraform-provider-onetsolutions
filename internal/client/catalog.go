package client

import (
	"context"
	"net/http"
	"net/url"
)

// InstanceSize is an entry of GET /v1/compute/sizes. The schema publishes no response type; the
// fields follow what the API serves.
type InstanceSize struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Slug             string               `json:"slug"`
	CPUModel         string               `json:"cpu_model"`
	VCPU             int64                `json:"vcpu"`
	RAMMB            int64                `json:"ram_mb"`
	BandwidthMbps    int64                `json:"bandwidth_mbps"`
	IsActive         bool                 `json:"is_active"`
	Description      string               `json:"description"`
	AvailableVolumes []SizeVolume         `json:"available_volumes"`
	PricingOptions   []SizePricingOption  `json:"pricing_options"`
	AvailableAddons  []SizeAvailableAddon `json:"available_addons"`
}

// SizeVolume is a volume configuration a size can be installed on.
type SizeVolume struct {
	ID         string `json:"id"`
	VolumeType string `json:"volume_type"`
	DiskGB     int64  `json:"disk_gb"`
	IsDefault  bool   `json:"is_default"`
	IsActive   bool   `json:"is_active"`
}

// SizePricingOption is the price of a size for one billing cycle.
type SizePricingOption struct {
	ID              string `json:"id"`
	BillingCycle    string `json:"billing_cycle"`
	Price           string `json:"price"`
	DiscountPercent string `json:"discount_percent"`
	IsDefault       bool   `json:"is_default"`
	IsActive        bool   `json:"is_active"`
}

// SizeAvailableAddon is an add-on that can be ordered with a size.
type SizeAvailableAddon struct {
	ID         string    `json:"id"`
	Addon      SizeAddon `json:"addon"`
	IsIncluded bool      `json:"is_included"`
	IsDefault  bool      `json:"is_default"`
	IsActive   bool      `json:"is_active"`
	IsHidden   bool      `json:"is_hidden"`
}

// SizeAddon describes an add-on.
type SizeAddon struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	BillingType string `json:"billing_type"`
	IsActive    bool   `json:"is_active"`
}

// Image is the InstanceImage schema.
type Image struct {
	ID                   string  `json:"id"`
	OrganizationID       *string `json:"organization_id"`
	Name                 string  `json:"name"`
	Slug                 string  `json:"slug"`
	Description          string  `json:"description"`
	Visibility           string  `json:"visibility"`
	OSName               string  `json:"os_name"`
	OSVersion            string  `json:"os_version"`
	OSArchitecture       string  `json:"os_architecture"`
	Language             string  `json:"language"`
	IsActive             bool    `json:"is_active"`
	IsFeatured           bool    `json:"is_featured"`
	IsWindows            bool    `json:"is_windows"`
	RequiresLicenseAddon bool    `json:"requires_license_addon"`
	MinDiskGB            int64   `json:"min_disk_gb"`
	MinRAMMB             int64   `json:"min_ram_mb"`
	Created              string  `json:"created"`
}

// ControlPanel is the PublicControlPanel schema.
type ControlPanel struct {
	ID                string              `json:"id"`
	Slug              string              `json:"slug"`
	Name              string              `json:"name"`
	Description       string              `json:"description"`
	AccessPort        int64               `json:"access_port"`
	CompatibleImages  []ControlPanelImage `json:"compatible_images"`
	MinVCPU           int64               `json:"min_vcpu"`
	MinRAMMB          int64               `json:"min_ram_mb"`
	MinDiskGB         int64               `json:"min_disk_gb"`
	InstallMinutesMin int64               `json:"install_minutes_min"`
	InstallMinutesMax int64               `json:"install_minutes_max"`
	DocsURL           string              `json:"docs_url"`
}

// ControlPanelImage is an image a control panel installs on.
type ControlPanelImage struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// HostingPlan is the WebHostingPlanList schema.
type HostingPlan struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Slug             string               `json:"slug"`
	Description      string               `json:"description"`
	HostingType      string               `json:"hosting_type"`
	GroupSlug        *string              `json:"group_slug"`
	DiskSpaceMB      int64                `json:"disk_space_mb"`
	BandwidthMB      int64                `json:"bandwidth_mb"`
	MaxEmailAccounts int64                `json:"max_email_accounts"`
	MaxDatabases     int64                `json:"max_databases"`
	MaxSubdomains    int64                `json:"max_subdomains"`
	MaxAddonDomains  int64                `json:"max_addon_domains"`
	MaxFTPAccounts   int64                `json:"max_ftp_accounts"`
	Pricing          []HostingPlanPricing `json:"pricing"`
	IsActive         bool                 `json:"is_active"`
}

// HostingPlanPricing is the price of a hosting plan for one billing cycle.
type HostingPlanPricing struct {
	BillingCycle    string `json:"billing_cycle"`
	Price           string `json:"price"`
	DiscountPercent string `json:"discount_percent"`
	IsDefault       bool   `json:"is_default"`
}

// WebHosting is the WebHosting schema, reduced to what the data source exposes.
type WebHosting struct {
	ID               string             `json:"id"`
	Domain           string             `json:"domain"`
	Username         string             `json:"username"`
	Status           string             `json:"status"`
	Plan             *WebHostingPlanRef `json:"plan"`
	DiskUsageMB      int64              `json:"disk_usage_mb"`
	DiskLimitMB      int64              `json:"disk_limit_mb"`
	BandwidthUsageMB int64              `json:"bandwidth_usage_mb"`
	BandwidthLimitMB int64              `json:"bandwidth_limit_mb"`
	DedicatedIP      *string            `json:"dedicated_ip"`
	NS1              string             `json:"ns1"`
	NS2              string             `json:"ns2"`
	BillingCycle     string             `json:"billing_cycle"`
	Price            string             `json:"price"`
	NextBillingDate  *string            `json:"next_billing_date"`
	IsActive         bool               `json:"is_active"`
	Created          string             `json:"created"`
}

// WebHostingPlanRef is the plan embedded in a web hosting.
type WebHostingPlanRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// WebHostingSummary is the WebHostingList schema, what the list endpoint returns.
type WebHostingSummary struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Status string `json:"status"`
}

const (
	sizesPath       = "/v1/compute/sizes"
	projectImages   = "/v1/organizations/{organization_id}/projects/{project_id}/compute/images"
	webHostingsPath = "/v1/organizations/{organization_id}/projects/{project_id}/hosting"
)

var (
	routeListSizes         = newRoute(http.MethodGet, sizesPath)
	routeGetSize           = newRoute(http.MethodGet, sizesPath+"/{id}")
	routeListPublicImages  = newRoute(http.MethodGet, "/v1/compute/images/public")
	routeListProjectImages = newRoute(http.MethodGet, projectImages)
	routeGetProjectImage   = newRoute(http.MethodGet, projectImages+"/{id}")
	routeListControlPanels = newRoute(http.MethodGet, "/v1/compute/control-panels")
	routeListHostingPlans  = newRoute(http.MethodGet, "/v1/hosting/plans")
	routeListWebHostings   = newRoute(http.MethodGet, webHostingsPath)
	routeGetWebHosting     = newRoute(http.MethodGet, webHostingsPath+"/{id}")
)

// ListInstanceSizes lists the instance sizes on sale.
func (c *Client) ListInstanceSizes(ctx context.Context) ([]InstanceSize, error) {
	return listAll[InstanceSize](ctx, c, routeListSizes, nil, nil)
}

// GetInstanceSize reads one instance size.
func (c *Client) GetInstanceSize(ctx context.Context, id string) (*InstanceSize, error) {
	var size InstanceSize
	if err := c.do(ctx, routeGetSize, Params{"id": id}, nil, &size); err != nil {
		return nil, err
	}
	return &size, nil
}

// ListPublicImages lists the public images, anonymously.
func (c *Client) ListPublicImages(ctx context.Context) ([]Image, error) {
	return listAll[Image](ctx, c, routeListPublicImages, nil, nil)
}

// ListProjectImages lists the images a project can install: public and private ones, or only
// those of visibility when it is "public" or "private".
func (c *Client) ListProjectImages(ctx context.Context, scope Scope, visibility string) ([]Image, error) {
	var query url.Values
	if visibility != "" {
		query = url.Values{"visibility": {visibility}}
	}
	return listAll[Image](ctx, c, routeListProjectImages, scope.Params(), query)
}

// GetProjectImage reads one image a project can install, public or private.
func (c *Client) GetProjectImage(ctx context.Context, scope Scope, id string) (*Image, error) {
	var image Image
	if err := c.do(ctx, routeGetProjectImage, scope.Params("id", id), nil, &image); err != nil {
		return nil, err
	}
	return &image, nil
}

// ListControlPanels lists the control panels an instance can be installed with.
func (c *Client) ListControlPanels(ctx context.Context) ([]ControlPanel, error) {
	return listAll[ControlPanel](ctx, c, routeListControlPanels, nil, nil)
}

// ListHostingPlans lists the web hosting plans on sale.
func (c *Client) ListHostingPlans(ctx context.Context) ([]HostingPlan, error) {
	return listAll[HostingPlan](ctx, c, routeListHostingPlans, nil, nil)
}

// ListWebHostings lists the web hostings of a project.
func (c *Client) ListWebHostings(ctx context.Context, scope Scope) ([]WebHostingSummary, error) {
	return listAll[WebHostingSummary](ctx, c, routeListWebHostings, scope.Params(), nil)
}

// GetWebHosting reads one web hosting.
func (c *Client) GetWebHosting(ctx context.Context, scope Scope, id string) (*WebHosting, error) {
	var hosting WebHosting
	if err := c.do(ctx, routeGetWebHosting, scope.Params("id", id), nil, &hosting); err != nil {
		return nil, err
	}
	return &hosting, nil
}
