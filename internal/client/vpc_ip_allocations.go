package client

import (
	"context"
	"net/http"
)

// VPCIPAllocation is one private address of a VPC (the VPCIPAllocation schema).
type VPCIPAllocation struct {
	ID             string  `json:"id"`
	VPCID          string  `json:"vpc_id"`
	IPAddress      string  `json:"ip_address"`
	InstanceID     *string `json:"instance_id"`
	InstanceName   *string `json:"instance_name"`
	Status         string  `json:"status"`
	AllocationType string  `json:"allocation_type"`
	InterfaceIndex int64   `json:"interface_index"`
	AllocatedAt    string  `json:"allocated_at"`
}

var routeListVPCIPAllocations = newRoute(http.MethodGet, vpcsPath+"/{vpc_id}/ip-allocations")

// ListVPCIPAllocations lists the addresses allocated or reserved in a VPC, ordered by address.
func (c *Client) ListVPCIPAllocations(ctx context.Context, scope Scope, vpcID string) ([]VPCIPAllocation, error) {
	return listAll[VPCIPAllocation](ctx, c, routeListVPCIPAllocations, scope.Params("vpc_id", vpcID), nil)
}
