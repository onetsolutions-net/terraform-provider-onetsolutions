// Package account holds the data sources of organizations and projects. Neither can be created
// through the public API, so there are no resources.
package account

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this area.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{}
}

// DataSources returns the data sources of this area.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrganizationDataSource,
		NewOrganizationsDataSource,
		NewProjectDataSource,
		NewProjectsDataSource,
	}
}
