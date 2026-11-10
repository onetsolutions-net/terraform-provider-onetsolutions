// Package dns holds the DNS zones, their records and DNSSEC signing, and the settings of the
// domains registered in a project.
package dns

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this area.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{
		NewZoneResource,
		NewRecordResource,
		NewDNSSECResource,
		NewDomainSettingsResource,
	}
}

// DataSources returns the data sources of this area.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewZoneDataSource,
		NewDomainDataSource,
	}
}
