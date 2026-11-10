package client

import (
	"context"
	"net/http"
	"net/url"
)

// Organization is the Organization schema.
type Organization struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	OrganizationType string `json:"organization_type"`
	SupportPlan      string `json:"support_plan"`
	CompanyName      string `json:"company_name"`
	Country          string `json:"country"`
	ProjectQuota     int64  `json:"project_quota"`
	ProjectCount     int64  `json:"project_count"`
	IsActive         bool   `json:"is_active"`
	Created          string `json:"created"`
}

// Project is the Project schema.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DomainQuota int64  `json:"domain_quota"`
}

var (
	routeListOrganizations = newRoute(http.MethodGet, "/v1/organizations")
	routeGetOrganization   = newRoute(http.MethodGet, "/v1/organizations/{id}")
	routeListProjects      = newRoute(http.MethodGet, "/v1/organizations/{organization_id}/projects")
	routeGetProject        = newRoute(http.MethodGet, "/v1/organizations/{organization_id}/projects/{id}")
)

// ListOrganizations lists the organizations the API key's user is a member of.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	return listAll[Organization](ctx, c, routeListOrganizations, nil, url.Values{"member": {"true"}})
}

// GetOrganization reads one organization.
func (c *Client) GetOrganization(ctx context.Context, id string) (*Organization, error) {
	var org Organization
	if err := c.do(ctx, routeGetOrganization, Params{"id": id}, nil, &org); err != nil {
		return nil, err
	}
	return &org, nil
}

// ListProjects lists the projects of an organization the user can access.
func (c *Client) ListProjects(ctx context.Context, organizationID string) ([]Project, error) {
	return listAll[Project](ctx, c, routeListProjects, Params{"organization_id": organizationID}, nil)
}

// GetProject reads one project.
func (c *Client) GetProject(ctx context.Context, organizationID, id string) (*Project, error) {
	var project Project
	err := c.do(ctx, routeGetProject, Params{"organization_id": organizationID, "id": id}, nil, &project)
	if err != nil {
		return nil, err
	}
	return &project, nil
}
