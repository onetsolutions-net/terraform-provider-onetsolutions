// Package base holds the reference resources every area copies: onetsolutions_ssh_key and
// onetsolutions_vpc, with their data sources.
package base

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this package.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{
		NewSSHKeyResource,
		NewVPCResource,
	}
}

// DataSources returns the data sources of this package.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewSSHKeyDataSource,
		NewVPCDataSource,
	}
}
