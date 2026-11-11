package fakeapi

import (
	"net/http"
	"slices"
	"strings"
)

// KindVPCIPAllocation is a private address of a VPC.
const KindVPCIPAllocation Kind = "vpc_ip_allocation" // parent: VPC

// registerVPCIPAllocations serves the private addresses of a VPC as the API does: every
// allocation not released, ordered by address, paginated; 404 for a VPC outside the project.
func registerVPCIPAllocations(s *Server) {
	s.handle(http.MethodGet, vpcsTemplate+"/{vpc_id}/ip-allocations", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		vpcID := r.PathValue("vpc_id")
		if _, ok := s.lookupQuiet(KindVPC, scope, "", vpcID); !ok {
			writeNotFound(w)
			return
		}
		var kept []Object
		for _, allocation := range s.list(KindVPCIPAllocation, scope, vpcID) {
			if allocation.String("status") != "released" {
				kept = append(kept, allocation)
			}
		}
		slices.SortStableFunc(kept, func(a, b Object) int { return strings.Compare(a.String("ip_address"), b.String("ip_address")) })
		writePage(w, r, kept)
	}))
}

// SeedVPCIPAllocation adds a private address to a VPC. The override "instance_id" names an
// instance of the project, whose name the allocation carries; without it, the address is the
// VPC's gateway.
func (s *Server) SeedVPCIPAllocation(scope Scope, vpcID string, overrides Object) Object {
	vpc, ok := s.lookupQuiet(KindVPC, scope, "", vpcID)
	if !ok {
		s.t.Fatalf("fakeapi: no VPC %s", vpcID)
	}
	base := Object{
		"vpc_id":          vpcID,
		"vpc_name":        vpc.String("name"),
		"ip_address":      "10.10.0.1",
		"instance_id":     nil,
		"instance_name":   nil,
		"status":          "allocated",
		"allocation_type": "gateway",
		"interface_index": 1,
		"allocated_at":    now(),
		"released_at":     nil,
	}
	if instanceID, ok := overrides["instance_id"].(string); ok {
		instance, found := s.lookupQuiet(KindInstance, scope, "", instanceID)
		if !found {
			s.t.Fatalf("fakeapi: no instance %s", instanceID)
		}
		base["instance_name"] = instance.String("name")
		base["allocation_type"] = "instance"
	}
	return s.insert(KindVPCIPAllocation, scope, vpcID, merge(base, overrides))
}
