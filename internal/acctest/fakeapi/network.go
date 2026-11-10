package fakeapi

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	firewallTemplate    = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/firewall"
	snapshotsTemplate   = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/snapshots"
	ipAddressesTemplate = "/v1/organizations/{organization_id}/projects/{project_id}/compute/instances/{instance_id}/ip-addresses"
)

// Task types of the network area.
const (
	TaskApplyFirewall    = "apply_firewall"
	TaskCreateSnapshot   = "create_snapshot"
	TaskDeleteSnapshot   = "delete_snapshot"
	TaskUpdateReverseDNS = "update_reverse_dns"
)

var (
	snapshotNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)
	ptrHostnamePattern  = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	portPartPattern     = regexp.MustCompile(`^(\d+)(?:[-:](\d+))?$`)
	firewallDirections  = map[string]string{"in": "inbound", "out": "outbound", "inbound": "inbound", "outbound": "outbound"}
	firewallActions     = []string{"accept", "drop", "reject"}
	firewallProtocols   = []string{"tcp", "udp", "icmp", "all"}
)

const (
	portError             = "Use ports from 1 to 65535: a port, a range like 1000-2000, or a comma-separated list. Leave empty for all ports."
	portlessProtocolError = "Ports only apply to TCP and UDP. Leave the port empty for ICMP or all protocols."
	taskInProgressDetail  = "A task is already in progress on this instance. Please try again once it has finished."
	instanceLockedDetail  = "An operation is running on this instance. Please try again once it has finished."
)

// registerNetwork serves the firewall, snapshots and IP addresses of an instance, as the API
// does.
//
// Like the API, every change to a firewall rule queues an apply_firewall task, and every
// mutation is refused with 409 instance_task_in_progress while a task of the instance runs.
// Nothing polls those implicit tasks, so each refused request advances them one step: an
// instance stays busy for a few requests, as it does for a few seconds in the real API.
func registerNetwork(s *Server) {
	s.handle(http.MethodGet, firewallTemplate, s.withInstance(func(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
		writePage(w, r, s.firewallRulesOf(scope, instanceID))
	}))
	s.handle(http.MethodPost, firewallTemplate, s.withInstance(s.createFirewallRule))
	s.handle(http.MethodGet, firewallTemplate+"/{id}", s.withInstance(func(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
		s.writeObject(w, KindFirewallRule, scope, instanceID, r.PathValue("id"))
	}))
	s.handle(http.MethodPatch, firewallTemplate+"/{id}", s.withInstance(s.updateFirewallRule))
	s.handle(http.MethodDelete, firewallTemplate+"/{id}", s.withInstance(s.deleteFirewallRule))
	s.handle(http.MethodPost, firewallTemplate+"/apply", s.withInstance(s.applyFirewall))

	s.handle(http.MethodPost, snapshotsTemplate, s.withInstance(s.createSnapshot))
	s.handle(http.MethodGet, snapshotsTemplate+"/{id}", s.withInstance(func(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
		snapshot, ok := s.snapshotOf(scope, instanceID, r.PathValue("id"))
		if !ok || snapshot["status"] == "deleted" {
			writeNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	}))
	s.handle(http.MethodDelete, snapshotsTemplate+"/{id}", s.withInstance(s.deleteSnapshot))

	s.handle(http.MethodGet, ipAddressesTemplate, s.withInstance(func(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
		addresses := s.list(KindIPAddress, scope, instanceID)
		sort.SliceStable(addresses, func(i, j int) bool {
			if addresses[i]["is_primary"] != addresses[j]["is_primary"] {
				return addresses[i]["is_primary"] == true
			}
			return addresses[i].String("address") < addresses[j].String("address")
		})
		writePage(w, r, addresses)
	}))
	s.handle(http.MethodGet, ipAddressesTemplate+"/{id}", s.withInstance(func(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
		s.writeObject(w, KindIPAddress, scope, instanceID, r.PathValue("id"))
	}))
	s.handle(http.MethodPatch, ipAddressesTemplate+"/{id}", s.withInstance(s.updateReverseDNS))
}

// withInstance resolves the scope and the instance of the path (404 when the instance is not in
// that project) and refuses a change to an instance that is not active (reads stay allowed), as
// the API does.
func (s *Server) withInstance(handler func(http.ResponseWriter, *http.Request, Scope, string)) http.HandlerFunc {
	return s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		instanceID := r.PathValue("instance_id")
		instance, ok := s.lookupQuiet(KindInstance, scope, "", instanceID)
		if !ok {
			writeNotFound(w)
			return
		}
		if r.Method != http.MethodGet && refuseInactive(w, instance) {
			return
		}
		handler(w, r, scope, instanceID)
	})
}

// refuseWhenBusy answers 409 while the instance is locked or one of its tasks runs, as the API's
// refuse_when_locked and refuse_when_busy do, and reports whether it did.
func (s *Server) refuseWhenBusy(w http.ResponseWriter, scope Scope, instanceID string) bool {
	instance, _ := s.lookupQuiet(KindInstance, scope, "", instanceID)
	if reason, _ := instance["lock_reason"].(string); reason != "" {
		writeJSON(w, http.StatusConflict, Object{
			"detail": instanceLockedDetail, "code": "instance_locked", "error": instanceLockedDetail,
			"context": Object{"lock_reason": reason},
		})
		return true
	}
	active := s.activeTasksOf(scope, instanceID)
	if len(active) == 0 {
		return false
	}
	for _, task := range active {
		s.advanceTask(task.ID())
	}
	writeJSON(w, http.StatusConflict, Object{
		"detail": taskInProgressDetail, "code": "instance_task_in_progress", "error": taskInProgressDetail,
		"context": Object{"active_task_type": active[0]["task_type"]},
	})
	return true
}

func (s *Server) activeTasksOf(scope Scope, instanceID string) []Object {
	tasks := s.list(KindInstanceTask, scope, instanceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	var active []Object
	for _, task := range tasks {
		if state, tracked := s.tasks[task.ID()]; tracked && !state.finished {
			active = append(active, task)
		}
	}
	return active
}

func (s *Server) firewallRulesOf(scope Scope, instanceID string) []Object {
	rules := s.list(KindFirewallRule, scope, instanceID)
	sort.SliceStable(rules, func(i, j int) bool { return number(rules[i]["priority"]) < number(rules[j]["priority"]) })
	return rules
}

func number(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func (s *Server) syncFirewall(scope Scope, instanceID string) Object {
	return s.newInstanceTask(scope, instanceID, TaskApplyFirewall, Object{}, nil)
}

// sourceLabel labels a rule from the control plane's egress address, like the API.
func sourceLabel(cidr string) any {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || prefix.Masked().String() != ControlPlaneEgressCIDR {
		return nil
	}
	return ControlPlaneLabel
}

func (s *Server) createFirewallRule(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	name := errs.requireString(body, "name", 255)
	direction := errs.requireChoice(body, "direction", firewallDirections)
	action := errs.requireChoice(body, "action", choices(firewallActions))
	protocol := errs.requireChoice(body, "protocol", choices(firewallProtocols))
	description := trimmedField(body, "description")
	source := "0.0.0.0/0"
	if raw, present := stringField(body, "source"); present {
		source = raw
		if !validNetwork(source) {
			errs.add("source", "Invalid CIDR notation: "+source)
		}
	}
	port := ""
	if raw, present := stringField(body, "destination_port"); present {
		port = errs.portSpec("destination_port", raw)
	}
	priority, isInt := integerField(body, "priority")
	switch {
	case body["priority"] == nil:
		errs.add("priority", "This field is required.")
	case !isInt:
		errs.add("priority", "A valid integer is required.")
	case priority < 1 || priority > 10000:
		errs.add("priority", "Ensure this value is between 1 and 10000.")
	}
	if len(errs) == 0 && port != "" && protocol != "tcp" && protocol != "udp" {
		errs.add("destination_port", portlessProtocolError)
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	rule := s.insert(KindFirewallRule, scope, instanceID, Object{
		"instance":         instanceID,
		"name":             name,
		"description":      description,
		"direction":        direction,
		"action":           action,
		"protocol":         protocol,
		"destination_port": port,
		"source_cidr":      source,
		"source_label":     sourceLabel(source),
		"destination_cidr": "",
		"priority":         priority,
		"is_enabled":       true,
	})
	s.syncFirewall(scope, instanceID)
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) updateFirewallRule(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	id := r.PathValue("id")
	current, ok := s.lookupQuiet(KindFirewallRule, scope, instanceID, id)
	if !ok {
		writeNotFound(w)
		return
	}
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	changes := Object{}
	if _, present := body["name"]; present {
		changes["name"] = errs.requireString(body, "name", 255)
	}
	if raw, present := body["description"]; present {
		if text, isString := raw.(string); isString {
			changes["description"] = strings.TrimSpace(text)
		} else {
			errs.add("description", "Not a valid string.")
		}
	}
	if _, present := body["direction"]; present {
		changes["direction"] = errs.requireChoice(body, "direction", choices([]string{"inbound", "outbound"}))
	}
	if _, present := body["action"]; present {
		changes["action"] = errs.requireChoice(body, "action", choices(firewallActions))
	}
	if _, present := body["protocol"]; present {
		changes["protocol"] = errs.requireChoice(body, "protocol", choices(firewallProtocols))
	}
	if raw, present := stringField(body, "destination_port"); present {
		changes["destination_port"] = errs.portSpec("destination_port", raw)
	}
	if raw, present := stringField(body, "source_cidr"); present {
		if !validNetwork(raw) {
			errs.add("source_cidr", "Invalid CIDR notation: "+raw)
		}
		changes["source_cidr"] = raw
		changes["source_label"] = sourceLabel(raw)
	}
	if raw, present := stringField(body, "destination_cidr"); present {
		if raw != "" && !validNetwork(raw) {
			errs.add("destination_cidr", "Invalid CIDR notation: "+raw)
		}
		changes["destination_cidr"] = raw
	}
	if _, present := body["priority"]; present {
		priority, isInt := integerField(body, "priority")
		if !isInt || priority < 0 {
			errs.add("priority", "A valid integer is required.")
		}
		changes["priority"] = priority
	}
	if raw, present := body["is_enabled"]; present {
		enabled, isBool := raw.(bool)
		if !isBool {
			errs.add("is_enabled", "Must be a valid boolean.")
		}
		changes["is_enabled"] = enabled
	}
	if len(errs) == 0 {
		protocol, hasProtocol := changes["protocol"].(string)
		port, hasPort := changes["destination_port"].(string)
		if hasProtocol || hasPort {
			if !hasProtocol {
				protocol = current.String("protocol")
			}
			if !hasPort {
				port = current.String("destination_port")
			}
			if port != "" && protocol != "tcp" && protocol != "udp" {
				errs.add("destination_port", portlessProtocolError)
			}
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	updated, found := s.update(KindFirewallRule, scope, instanceID, id, func(o Object) {
		for key, value := range changes {
			o[key] = value
		}
	})
	if !found {
		writeNotFound(w)
		return
	}
	s.syncFirewall(scope, instanceID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteFirewallRule(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	id := r.PathValue("id")
	if _, ok := s.lookupQuiet(KindFirewallRule, scope, instanceID, id); !ok {
		writeNotFound(w)
		return
	}
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	s.remove(KindFirewallRule, scope, instanceID, id)
	s.syncFirewall(scope, instanceID)
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) applyFirewall(w http.ResponseWriter, _ *http.Request, scope Scope, instanceID string) {
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	task := s.syncFirewall(scope, instanceID)
	writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
}

func (s *Server) createSnapshot(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	raw := errs.requireString(body, "name", 255)
	name := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case raw == "":
	case len(name) < 3:
		errs.add("name", "Snapshot name must be at least 3 characters long.")
	case name[0] >= '0' && name[0] <= '9':
		errs.add("name", "Snapshot name cannot start with a number.")
	case !snapshotNamePattern.MatchString(name):
		errs.add("name", "Snapshot name may only contain letters, digits, hyphens and underscores.")
	}
	description := trimmedField(body, "description")
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	for _, existing := range s.list(KindSnapshot, scope, instanceID) {
		if existing["name"] == name {
			writeFieldErrors(w, fieldErrors{"name": {"A snapshot with this name already exists for this instance."}})
			return
		}
	}
	snapshot := s.insert(KindSnapshot, scope, instanceID, Object{
		"instance_id": instanceID,
		"name":        name,
		"description": description,
		"size_gb":     nil,
		"status":      "pending",
	})
	snapshotID := snapshot.ID()
	task := s.newInstanceTask(scope, instanceID, TaskCreateSnapshot,
		Object{"snapshot_id": snapshotID, "snapshot_name": name},
		func() {
			s.update(KindSnapshot, scope, instanceID, snapshotID, func(o Object) {
				o["status"] = "present"
				o["size_gb"] = "2.35"
			})
		})
	snapshot["task_id"] = task.ID()
	writeJSON(w, http.StatusCreated, snapshot)
}

// snapshotOf reads a snapshot and, while it is pending, gives it the "failed" status once its
// create_snapshot task has failed, as the API's task does.
func (s *Server) snapshotOf(scope Scope, instanceID, id string) (Object, bool) {
	snapshot, ok := s.lookup(KindSnapshot, scope, instanceID, id)
	if !ok || snapshot["status"] != "pending" {
		return snapshot, ok
	}
	for _, task := range s.list(KindInstanceTask, scope, instanceID) {
		if task["task_type"] == TaskCreateSnapshot && task.Map("parameters").String("snapshot_id") == id && task["status"] == "failed" {
			return s.update(KindSnapshot, scope, instanceID, id, func(o Object) { o["status"] = "failed" })
		}
	}
	return snapshot, true
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	id := r.PathValue("id")
	snapshot, ok := s.lookupQuiet(KindSnapshot, scope, instanceID, id)
	if !ok || snapshot["status"] == "deleted" {
		writeNotFound(w)
		return
	}
	s.update(KindSnapshot, scope, instanceID, id, func(o Object) { o["status"] = "deleting" })
	task := s.newInstanceTask(scope, instanceID, TaskDeleteSnapshot,
		Object{"snapshot_id": id, "snapshot_name": snapshot["name"]},
		func() { s.remove(KindSnapshot, scope, instanceID, id) })
	writeJSON(w, http.StatusAccepted, Object{"task_id": task.ID()})
}

func (s *Server) updateReverseDNS(w http.ResponseWriter, r *http.Request, scope Scope, instanceID string) {
	id := r.PathValue("id")
	address, ok := s.lookupQuiet(KindIPAddress, scope, instanceID, id)
	if !ok {
		writeNotFound(w)
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	requested := errs.requireString(body, "reverse_dns", 255)
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	if s.refuseWhenBusy(w, scope, instanceID) {
		return
	}
	if address["ip_type"] == "ipv6" {
		writeError(w, http.StatusBadRequest, "Reverse DNS can only be set on IPv4 addresses for now.", "reverse_dns_unsupported")
		return
	}
	hostname := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(requested), "."))
	if len(hostname) > 253 || !ptrHostnamePattern.MatchString(hostname) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("'%s' is not a fully qualified hostname such as mail.example.com.", requested), "reverse_dns_target_invalid")
		return
	}
	if hostname != address["reverse_dns"] {
		s.newInstanceTask(scope, instanceID, TaskUpdateReverseDNS,
			Object{"ip_address_id": id, "ip_address": address["address"], "reverse_dns": hostname},
			func() { s.setReverseDNS(scope, instanceID, id, hostname) })
	}
	writeJSON(w, http.StatusOK, address)
}

// setReverseDNS stores the hostname on the address and on the copy the instance embeds.
func (s *Server) setReverseDNS(scope Scope, instanceID, id, hostname string) {
	s.update(KindIPAddress, scope, instanceID, id, func(o Object) { o["reverse_dns"] = hostname })
	addresses := s.list(KindIPAddress, scope, instanceID)
	s.update(KindInstance, scope, "", instanceID, func(o Object) { o["ip_addresses"] = addresses })
}

func choices(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, value := range values {
		out[value] = value
	}
	return out
}

// requireChoice checks body[key] is one of the keys of allowed and returns its canonical value.
func (f fieldErrors) requireChoice(body Object, key string, allowed map[string]string) string {
	raw, present := body[key]
	if !present || raw == nil {
		f.add(key, "This field is required.")
		return ""
	}
	value, _ := raw.(string)
	canonical, ok := allowed[value]
	if !ok {
		f.add(key, fmt.Sprintf("%q is not a valid choice.", value))
		return ""
	}
	return canonical
}

// portSpec checks a port spec as the API does: spaces removed, then ports, ranges and lists.
func (f fieldErrors) portSpec(key, value string) string {
	cleaned := strings.ReplaceAll(value, " ", "")
	if cleaned == "" {
		return ""
	}
	for _, part := range strings.Split(cleaned, ",") {
		match := portPartPattern.FindStringSubmatch(part)
		if match == nil {
			f.add(key, portError)
			return cleaned
		}
		low, _ := strconv.Atoi(match[1])
		high := low
		if match[2] != "" {
			high, _ = strconv.Atoi(match[2])
		}
		if low < 1 || low > high || high > 65535 {
			f.add(key, portError)
			return cleaned
		}
	}
	return cleaned
}

func integerField(body Object, key string) (int64, bool) {
	value, ok := body[key].(float64)
	if !ok || value != float64(int64(value)) {
		return 0, false
	}
	return int64(value), true
}

func validNetwork(value string) bool {
	if _, err := netip.ParsePrefix(value); err == nil {
		return true
	}
	_, err := netip.ParseAddr(value)
	return err == nil
}
