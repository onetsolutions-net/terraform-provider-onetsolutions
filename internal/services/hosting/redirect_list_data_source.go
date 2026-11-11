package hosting

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// NewRedirectsDataSource returns the onetsolutions_hosting_redirects data source.
func NewRedirectsDataSource() datasource.DataSource {
	return &webListDataSource[client.HostingRedirect]{
		typeName:    "_hosting_redirects",
		description: "Lists the redirects of a web hosting.",
		listName:    "redirects",
		listWhat:    "The redirects.",
		attributes: map[string]string{
			"domain":      "The domain the redirect applies to.",
			"source":      "The path redirected, starting with `/`.",
			"destination": "The URL redirected to.",
			"type":        "`301` (permanent) or `302` (temporary).",
		},
		list: func(c *client.Client) webListFunc[client.HostingRedirect] { return c.ListHostingRedirects },
		values: func(r *client.HostingRedirect) map[string]string {
			return map[string]string{
				"domain": r.Domain, "source": redirectSource(r.Source), "destination": r.Destination, "type": redirectType(r.Type),
			}
		},
	}
}
