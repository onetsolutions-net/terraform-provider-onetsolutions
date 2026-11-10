// Package network holds the resources attached to an instance's network and disks:
// onetsolutions_instance_firewall, onetsolutions_instance_snapshot and
// onetsolutions_instance_ip_reverse_dns.
package network

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Resources returns the resources of this area.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{
		NewInstanceFirewallResource,
		NewInstanceSnapshotResource,
		NewInstanceIPReverseDNSResource,
	}
}

// DataSources returns the data sources of this area.
func DataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
