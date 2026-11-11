// Package hosting holds the web hostings of a project and what lives inside them: PHP versions,
// WordPress sites, cron jobs, subdomains, addon domains, redirects, databases and their users,
// mailboxes and FTP accounts.
package hosting

import (
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this area. Each group of objects lists its own in a
// register_<group>.go file.
func Resources() []func() resource.Resource {
	return slices.Concat(
		serviceResources(),
		webResources(),
		mailDBResources(),
	)
}

// DataSources returns the data sources of this area.
func DataSources() []func() datasource.DataSource {
	return slices.Concat(
		serviceDataSources(),
		webDataSources(),
		mailDBDataSources(),
	)
}
