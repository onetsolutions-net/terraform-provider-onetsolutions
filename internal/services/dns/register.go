// Package dns holds the DNS zones, their records and DNSSEC signing, the registration of domains
// and their settings, and the domain catalog.
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
		NewDomainResource,
	}
}

// DataSources returns the data sources of this area.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewZoneDataSource,
		NewDomainDataSource,
		NewDomainAvailabilityDataSource,
		NewTLDsDataSource,
	}
}
