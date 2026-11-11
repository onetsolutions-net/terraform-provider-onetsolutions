package hosting

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// NewAddonDomainsDataSource returns the onetsolutions_hosting_addon_domains data source.
func NewAddonDomainsDataSource() datasource.DataSource {
	return &webListDataSource[client.HostingAddonDomain]{
		typeName:    "_hosting_addon_domains",
		description: "Lists the addon domains of a web hosting.",
		listName:    "addon_domains",
		listWhat:    "The addon domains.",
		attributes: map[string]string{
			"domain":        "The addon domain.",
			"subdomain":     "The internal subdomain of the main domain it is attached to.",
			"document_root": "The directory it serves, as an absolute path.",
		},
		list: func(c *client.Client) webListFunc[client.HostingAddonDomain] { return c.ListHostingAddonDomains },
		values: func(d *client.HostingAddonDomain) map[string]string {
			return map[string]string{"domain": d.Domain, "subdomain": d.Subdomain, "document_root": d.DocumentRoot}
		},
	}
}
