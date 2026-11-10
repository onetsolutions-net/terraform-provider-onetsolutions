// Package catalog holds the read-only data sources of what can be ordered: instance sizes, images,
// control panels, web hosting plans, and the web hostings of a project.
package catalog

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
		NewInstanceSizeDataSource,
		NewInstanceSizesDataSource,
		NewImageDataSource,
		NewImagesDataSource,
		NewControlPanelsDataSource,
		NewHostingPlansDataSource,
		NewWebHostingDataSource,
	}
}
