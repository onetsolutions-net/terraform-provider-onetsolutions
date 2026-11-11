package hosting

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// serviceResources are the web hosting itself, its PHP versions and its WordPress sites.
func serviceResources() []func() resource.Resource {
	return []func() resource.Resource{
		NewWebHostingResource,
		NewPHPVersionResource,
		NewWordPressResource,
	}
}

// serviceDataSources are the data sources of the web hosting itself.
func serviceDataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewPHPVersionsDataSource,
	}
}
