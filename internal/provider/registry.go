package provider

import (
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/account"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/base"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/catalog"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/compute"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/dns"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/network"
)

// allResources aggregates the resources of every area package. An area adds a resource to its
// own register.go, never here.
func allResources() []func() resource.Resource {
	return slices.Concat(
		base.Resources(),
		account.Resources(),
		catalog.Resources(),
		compute.Resources(),
		network.Resources(),
		dns.Resources(),
	)
}

// allDataSources aggregates the data sources of every area package.
func allDataSources() []func() datasource.DataSource {
	return slices.Concat(
		base.DataSources(),
		account.DataSources(),
		catalog.DataSources(),
		compute.DataSources(),
		network.DataSources(),
		dns.DataSources(),
	)
}
