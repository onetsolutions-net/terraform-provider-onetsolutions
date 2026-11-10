package compute

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// Power states of the `state` attribute.
const (
	stateRunning = "running"
	stateStopped = "stopped"
)

// billingCycles are the cycles an instance can be billed on. hourly cannot be ordered
// (the API refuses it) but is read from instances already billed hourly.
var billingCycles = []string{cycleHourly, "monthly", "quarterly", "semi_annually", "annually", "biennially", "triennially"}

const (
	cycleHourly         = "hourly"
	defaultBillingCycle = "monthly"
)

// tagPattern is the API's tag rule, without a
// trailing space, which the API strips; at most maxTags tags, unique ignoring case.
var tagPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9 ._-]{0,48}[A-Za-z0-9._-])?$`)

const maxTags = 20

// hostnamePattern is the API's hostname rule (RFC 1123 labels separated by dots), without the
// look-arounds RE2 does not support.
var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

var ipAddressAttrTypes = map[string]attr.Type{
	"id":          types.StringType,
	"address":     types.StringType,
	"ip_type":     types.StringType,
	"is_primary":  types.BoolType,
	"reverse_dns": types.StringType,
}

var volumeAttrTypes = map[string]attr.Type{
	"id":        types.StringType,
	"name":      types.StringType,
	"size_gb":   types.Int64Type,
	"disk_type": types.StringType,
}

func ipAddressesValue(addresses []client.InstanceAddress) (types.List, diag.Diagnostics) {
	elemType := types.ObjectType{AttrTypes: ipAddressAttrTypes}
	values := make([]attr.Value, 0, len(addresses))
	var diags diag.Diagnostics
	for i := range addresses {
		a := &addresses[i]
		value, d := types.ObjectValue(ipAddressAttrTypes, map[string]attr.Value{
			"id":          types.StringValue(a.ID),
			"address":     types.StringValue(a.Address),
			"ip_type":     types.StringValue(a.IPType),
			"is_primary":  types.BoolValue(a.IsPrimary),
			"reverse_dns": types.StringValue(a.ReverseDNS),
		})
		diags.Append(d...)
		values = append(values, value)
	}
	list, d := types.ListValue(elemType, values)
	diags.Append(d...)
	return list, diags
}

func volumesValue(volumes []client.InstanceVolume) (types.List, diag.Diagnostics) {
	elemType := types.ObjectType{AttrTypes: volumeAttrTypes}
	values := make([]attr.Value, 0, len(volumes))
	var diags diag.Diagnostics
	for i := range volumes {
		v := &volumes[i]
		value, d := types.ObjectValue(volumeAttrTypes, map[string]attr.Value{
			"id":        types.StringValue(v.ID),
			"name":      types.StringValue(v.Name),
			"size_gb":   types.Int64Value(v.SizeGB),
			"disk_type": types.StringValue(v.DiskType),
		})
		diags.Append(d...)
		values = append(values, value)
	}
	list, d := types.ListValue(elemType, values)
	diags.Append(d...)
	return list, diags
}

func stringsSet(ctx context.Context, items []string) (types.Set, diag.Diagnostics) {
	if items == nil {
		items = []string{}
	}
	return types.SetValueFrom(ctx, types.StringType, items)
}

func setStrings(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	if set.IsNull() || set.IsUnknown() {
		return nil, nil
	}
	var items []string
	diags := set.ElementsAs(ctx, &items, false)
	return items, diags
}

// powerState maps the power state the API reads to the `state` attribute; ok is false for a
// transient or unknown state, which leaves the attribute as it is.
func powerState(vmStatus string) (state string, ok bool) {
	switch vmStatus {
	case client.VMStatusRunning:
		return stateRunning, true
	case client.VMStatusStopped, client.VMStatusShutoff:
		return stateStopped, true
	default:
		return "", false
	}
}

func optionalString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}
