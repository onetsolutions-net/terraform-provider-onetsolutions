package hosting

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// NewSubdomainsDataSource returns the onetsolutions_hosting_subdomains data source.
func NewSubdomainsDataSource() datasource.DataSource {
	return &webListDataSource[client.HostingSubdomain]{
		typeName:    "_hosting_subdomains",
		description: "Lists the subdomains of a web hosting.",
		listName:    "subdomains",
		listWhat:    "The subdomains.",
		attributes: map[string]string{
			"subdomain":   "The subdomain, without the main domain.",
			"domain":      "The main domain of the web hosting.",
			"full_domain": "The full domain name, such as `blog.example.com`.",
		},
		list: func(c *client.Client) webListFunc[client.HostingSubdomain] { return c.ListHostingSubdomains },
		values: func(s *client.HostingSubdomain) map[string]string {
			full, domain := strings.ToLower(s.FullDomain), strings.ToLower(s.Domain)
			return map[string]string{"subdomain": strings.TrimSuffix(full, "."+domain), "domain": domain, "full_domain": full}
		},
	}
}
