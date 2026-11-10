// Package common holds what every resource and data source shares: the provider data handed to
// them, the organization/project scoping attributes, import ID parsing, plan modifiers,
// timeouts, validators, custom types and error diagnostics.
package common

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

// Environment variables of the provider settings.
const (
	EnvAPIKey               = "ONETSOLUTIONS_API_KEY"
	EnvAPIURL               = "ONETSOLUTIONS_API_URL"
	EnvOrganizationID       = "ONETSOLUTIONS_ORGANIZATION_ID"
	EnvProjectID            = "ONETSOLUTIONS_PROJECT_ID"
	EnvMaxRequestsPerSecond = "ONETSOLUTIONS_MAX_REQUESTS_PER_SECOND"
)

// ProviderData is what the provider's Configure hands to every resource and data source.
type ProviderData struct {
	// Client is nil while NotReady is set.
	Client                *client.Client
	DefaultOrganizationID string
	DefaultProjectID      string
	Waiter                waiter.Config
	// NotReady explains why the provider could not build a client yet (a setting that depends
	// on a value known only after apply). Operations that need the API report it.
	NotReady string

	mu               sync.Mutex
	resolvedOrg      string
	resolvedProjects map[string]string
}

// Ready reports whether the provider has a client.
func (d *ProviderData) Ready() bool {
	return d != nil && d.Client != nil
}

// RequireClient returns the client, or adds an error diagnostic and returns nil.
func (d *ProviderData) RequireClient(diags *diag.Diagnostics) *client.Client {
	if d == nil {
		diags.AddError("Provider not configured",
			"The OnetSolutions provider has not been configured. This is a bug in the provider; please report it.")
		return nil
	}
	if d.Client == nil {
		diags.AddError("Provider not configured", d.NotReady)
		return nil
	}
	return d.Client
}

// ResolveScope returns the organization and project an object is created or looked up in: the
// value given (typically from the configuration or the plan), else the provider default, else
// the only organization of the API key's user / the only project of that organization. A null
// or unknown value counts as "not given".
func (d *ProviderData) ResolveScope(ctx context.Context, organizationID, projectID types.String) (client.Scope, diag.Diagnostics) {
	var diags diag.Diagnostics
	org, explicitOrg, orgDiags := d.resolveOrganization(ctx, organizationID)
	diags.Append(orgDiags...)
	if diags.HasError() {
		return client.Scope{}, diags
	}

	project := knownString(projectID)
	if project == "" && d.DefaultProjectID != "" && (!explicitOrg || org == d.DefaultOrganizationID) {
		project = d.DefaultProjectID
	}
	if project == "" {
		resolved, projectDiags := d.singleProject(ctx, org)
		diags.Append(projectDiags...)
		project = resolved
	}
	return client.Scope{OrganizationID: org, ProjectID: project}, diags
}

// ResolveOrganization is ResolveScope for objects that live in an organization only.
func (d *ProviderData) ResolveOrganization(ctx context.Context, organizationID types.String) (string, diag.Diagnostics) {
	org, _, diags := d.resolveOrganization(ctx, organizationID)
	return org, diags
}

func (d *ProviderData) resolveOrganization(ctx context.Context, organizationID types.String) (org string, explicit bool, diags diag.Diagnostics) {
	if configured := knownString(organizationID); configured != "" {
		return configured, true, nil
	}
	if d.DefaultOrganizationID != "" {
		return d.DefaultOrganizationID, false, nil
	}
	org, diags = d.singleOrganization(ctx)
	return org, false, diags
}

func (d *ProviderData) singleOrganization(ctx context.Context) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	c := d.RequireClient(&diags)
	if c == nil {
		return "", diags
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.resolvedOrg != "" {
		return d.resolvedOrg, nil
	}
	orgs, err := c.ListOrganizations(ctx)
	if err != nil {
		AddClientError(&diags, "Could not determine the organization", err)
		return "", diags
	}
	if len(orgs) != 1 {
		ids := make([]string, 0, len(orgs))
		for i := range orgs {
			ids = append(ids, fmt.Sprintf("%s (%s)", orgs[i].ID, orgs[i].Name))
		}
		diags.AddError("organization_id is required",
			fmt.Sprintf("The API key's user belongs to %d organizations, so the provider cannot pick one. "+
				"Set organization_id on this resource, or in the provider block (or the %s environment variable).%s",
				len(orgs), EnvOrganizationID, listSuffix(ids)))
		return "", diags
	}
	d.resolvedOrg = orgs[0].ID
	return d.resolvedOrg, nil
}

func (d *ProviderData) singleProject(ctx context.Context, organizationID string) (string, diag.Diagnostics) {
	var diags diag.Diagnostics
	c := d.RequireClient(&diags)
	if c == nil {
		return "", diags
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if project, ok := d.resolvedProjects[organizationID]; ok {
		return project, nil
	}
	projects, err := c.ListProjects(ctx, organizationID)
	if err != nil {
		AddClientError(&diags, "Could not determine the project", err)
		return "", diags
	}
	if len(projects) != 1 {
		ids := make([]string, 0, len(projects))
		for _, project := range projects {
			ids = append(ids, fmt.Sprintf("%s (%s)", project.ID, project.Name))
		}
		diags.AddError("project_id is required",
			fmt.Sprintf("Organization %s has %d projects, so the provider cannot pick one. "+
				"Set project_id on this resource, or in the provider block (or the %s environment variable).%s",
				organizationID, len(projects), EnvProjectID, listSuffix(ids)))
		return "", diags
	}
	if d.resolvedProjects == nil {
		d.resolvedProjects = map[string]string{}
	}
	d.resolvedProjects[organizationID] = projects[0].ID
	return projects[0].ID, nil
}

func knownString(v types.String) string {
	if !Known(v) {
		return ""
	}
	return v.ValueString()
}

func listSuffix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return " Available: " + strings.Join(items, ", ") + "."
}
