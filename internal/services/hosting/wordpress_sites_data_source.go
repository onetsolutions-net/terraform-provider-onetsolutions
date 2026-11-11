package hosting

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// NewWordPressSitesDataSource returns the onetsolutions_hosting_wordpress_sites data source.
func NewWordPressSitesDataSource() datasource.DataSource {
	return &webListDataSource[client.WordPressSite]{
		typeName:    "_hosting_wordpress_sites",
		description: "Lists the WordPress sites installed on a web hosting.",
		listName:    "wordpress_sites",
		listWhat:    "The WordPress sites of the web hosting.",
		attributes: map[string]string{
			"id":      "The identifier of the site, to import it as an `onetsolutions_hosting_wordpress`.",
			"domain":  "The domain the site is installed on.",
			"path":    "The path of the site under the domain, empty at its root.",
			"title":   "The title of the site.",
			"version": "The WordPress version installed, empty until the web hosting reports it.",
		},
		list: func(c *client.Client) webListFunc[client.WordPressSite] { return c.ListHostingWordPressSites },
		values: func(s *client.WordPressSite) map[string]string {
			return map[string]string{"id": s.ID, "domain": s.Domain, "path": s.Path, "title": s.Title, "version": s.Version}
		},
	}
}
