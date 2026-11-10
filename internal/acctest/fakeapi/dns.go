package fakeapi

import (
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

const (
	dnsZonesTemplate = "/v1/organizations/{organization_id}/projects/{project_id}/dns-zones"
	domainsTemplate  = "/v1/organizations/{organization_id}/projects/{project_id}/domains"
)

// ZoneNameservers are the name servers the fake assigns to a zone once it is provisioned.
var ZoneNameservers = []any{"ada.ns.onetsolutions.net", "bob.ns.onetsolutions.net"}

var (
	recordTypes      = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA", "PTR", "SOA"}
	proxiableTypes   = []string{"A", "AAAA", "CNAME"}
	hostTargetTypes  = []string{"CNAME", "MX", "NS", "PTR"}
	zoneLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Task types of the domain tasks the fake queues.
const (
	TaskUpdateNameservers   = "update_nameservers"
	TaskLockDomain          = "lock_domain"
	TaskUnlockDomain        = "unlock_domain"
	TaskEnableWhoisPrivacy  = "enable_whois_privacy"
	TaskDisableWhoisPrivacy = "disable_whois_privacy"
)

// registerDNS serves DNS zones, their records and DNSSEC state, and the settings of domains,
// as the API does. Like the API after a sync
// with the DNS service, it stores record names as lowercase FQDNs, TXT contents quoted and host
// targets without their trailing dot, whatever spelling the request used.
func registerDNS(s *Server) {
	s.handle(http.MethodGet, dnsZonesTemplate, s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		zones := newestFirst(s.list(KindDNSZone, scope, ""))
		for _, zone := range zones {
			delete(zone, "records")
		}
		writePage(w, r, zones)
	}))
	s.handle(http.MethodPost, dnsZonesTemplate, s.withScope(s.createDNSZone))
	s.handle(http.MethodGet, dnsZonesTemplate+"/{id}", s.withZone(func(w http.ResponseWriter, _ *http.Request, scope Scope, zone Object) {
		zone["records"] = s.list(KindDNSRecord, scope, zone.ID())
		writeJSON(w, http.StatusOK, zone)
	}))
	s.handle(http.MethodDelete, dnsZonesTemplate+"/{id}", s.withZone(func(w http.ResponseWriter, _ *http.Request, scope Scope, zone Object) {
		for _, record := range s.list(KindDNSRecord, scope, zone.ID()) {
			s.remove(KindDNSRecord, scope, zone.ID(), record.ID())
		}
		s.remove(KindDNSSEC, scope, zone.ID(), zone.ID())
		s.deleteObject(w, KindDNSZone, scope, "", zone.ID())
	}))

	s.handle(http.MethodGet, dnsZonesTemplate+"/{id}/records", s.withZone(func(w http.ResponseWriter, _ *http.Request, scope Scope, zone Object) {
		writeJSON(w, http.StatusOK, s.list(KindDNSRecord, scope, zone.ID()))
	}))
	s.handle(http.MethodPost, dnsZonesTemplate+"/{id}/records", s.withZone(s.createDNSRecord))
	s.handle(http.MethodGet, dnsZonesTemplate+"/{id}/records/{record_id}", s.withZone(func(w http.ResponseWriter, r *http.Request, scope Scope, zone Object) {
		s.writeObject(w, KindDNSRecord, scope, zone.ID(), r.PathValue("record_id"))
	}))
	s.handle(http.MethodPut, dnsZonesTemplate+"/{id}/records/{record_id}", s.withZone(s.updateDNSRecord))
	s.handle(http.MethodDelete, dnsZonesTemplate+"/{id}/records/{record_id}", s.withZone(func(w http.ResponseWriter, r *http.Request, scope Scope, zone Object) {
		s.deleteObject(w, KindDNSRecord, scope, zone.ID(), r.PathValue("record_id"))
		s.countRecords(scope, zone.ID())
	}))

	s.handle(http.MethodGet, dnsZonesTemplate+"/{id}/dnssec", s.withZone(func(w http.ResponseWriter, _ *http.Request, scope Scope, zone Object) {
		writeJSON(w, http.StatusOK, s.dnssecOf(scope, zone.ID()))
	}))
	s.handle(http.MethodPut, dnsZonesTemplate+"/{id}/dnssec", s.withZone(s.updateDNSSEC))

	s.handle(http.MethodGet, domainsTemplate, s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		domains := newestFirst(s.list(KindDomain, scope, ""))
		summaries := make([]Object, 0, len(domains))
		for _, domain := range domains {
			summaries = append(summaries, Object{
				"id": domain["id"], "name": domain["name"], "status": domain["status"],
				"registration_date": domain["registration_date"], "expiration_date": domain["expiration_date"],
				"auto_renew": domain["auto_renew"], "whois_privacy": domain["whois_privacy"],
				"is_active": domain["is_active"], "website_preview": nil, "created": domain["created"],
			})
		}
		writePage(w, r, summaries)
	}))
	s.handle(http.MethodGet, domainsTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		s.writeObject(w, KindDomain, scope, "", r.PathValue("id"))
	}))
	s.handle(http.MethodPut, domainsTemplate+"/{id}/nameservers", s.withDomain(s.updateDomainNameservers))
	s.handle(http.MethodPut, domainsTemplate+"/{id}/auto-renew", s.withDomain(s.setDomainAutoRenew))
	s.handle(http.MethodPost, domainsTemplate+"/{id}/lock", s.withDomain(s.domainFlagTask(TaskLockDomain, "is_locked", true)))
	s.handle(http.MethodPost, domainsTemplate+"/{id}/unlock", s.withDomain(s.domainFlagTask(TaskUnlockDomain, "is_locked", false)))
	s.handle(http.MethodPost, domainsTemplate+"/{id}/privacy/{action_type}", s.withDomain(func(w http.ResponseWriter, r *http.Request, scope Scope, domain Object) {
		switch r.PathValue("action_type") {
		case "enable":
			s.domainFlagTask(TaskEnableWhoisPrivacy, "whois_privacy", true)(w, r, scope, domain)
		case "disable":
			s.domainFlagTask(TaskDisableWhoisPrivacy, "whois_privacy", false)(w, r, scope, domain)
		default:
			writeNotFound(w)
		}
	}))
}

func (s *Server) withZone(handler func(http.ResponseWriter, *http.Request, Scope, Object)) http.HandlerFunc {
	return s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		zone, ok := s.lookup(KindDNSZone, scope, "", r.PathValue("id"))
		if !ok || zone["is_active"] != true {
			writeNotFound(w)
			return
		}
		handler(w, r, scope, zone)
	})
}

func (s *Server) withDomain(handler func(http.ResponseWriter, *http.Request, Scope, Object)) http.HandlerFunc {
	return s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		domain, ok := s.lookup(KindDomain, scope, "", r.PathValue("id"))
		if !ok || domain["is_active"] != true {
			writeNotFound(w)
			return
		}
		handler(w, r, scope, domain)
	})
}

// normalizeZoneName normalizes a zone name as the API does, reduced to its syntax checks.
func normalizeZoneName(raw string) (name, message string) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return "", "Enter a domain name such as example.com."
	}
	for _, label := range labels {
		if !zoneLabelPattern.MatchString(label) {
			return "", "Enter a domain name such as example.com."
		}
	}
	return name, ""
}

func (s *Server) createDNSZone(w http.ResponseWriter, r *http.Request, scope Scope) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	raw := errs.requireString(body, "zone_name", 254)
	mode := "onetsolutions"
	if value, present := stringField(body, "mode"); present {
		mode = value
	}
	if mode != "onetsolutions" && mode != "client" {
		errs.add("mode", fmt.Sprintf("%q is not a valid choice.", mode))
	}
	name := ""
	if raw != "" {
		normalized, message := normalizeZoneName(raw)
		if message != "" {
			errs.add("zone_name", message)
		}
		name = normalized
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	if mode == "client" {
		writeError(w, http.StatusBadRequest, "A zone hosted elsewhere is added by importing it.", "zone_mode_client_requires_import")
		return
	}
	for _, zone := range s.All(KindDNSZone) {
		if zone["zone_name"] == name {
			writeError(w, http.StatusConflict, "This DNS zone already exists.", "dns_zone_already_exists")
			return
		}
	}
	zone := s.insert(KindDNSZone, scope, "", Object{
		"zone_name":              name,
		"mode":                   mode,
		"nameservers":            []any{},
		"connected_account_name": nil,
		"domain_id":              nil,
		"is_active":              true,
		"is_verified":            false,
		"record_count":           0,
	})
	s.afterPolls(KindDNSZone, zone.ID(), s.taskPolls, func(o Object) {
		o["nameservers"] = ZoneNameservers
	})
	zone["records"] = []any{}
	writeJSON(w, http.StatusCreated, zone)
}

func (s *Server) countRecords(scope Scope, zoneID string) {
	count := len(s.list(KindDNSRecord, scope, zoneID))
	s.update(KindDNSZone, scope, "", zoneID, func(o Object) { o["record_count"] = count })
}

// recordFQDN is how the DNS service names a record: the lowercase FQDN without trailing dot.
func recordFQDN(name, zoneName string) string {
	cleaned := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	switch {
	case cleaned == "" || cleaned == "@" || cleaned == zoneName:
		return zoneName
	case strings.HasSuffix(cleaned, "."+zoneName):
		return cleaned
	default:
		return cleaned + "." + zoneName
	}
}

// maxTXTString is the most a TXT character-string holds (RFC 1035).
const maxTXTString = 255

// recordContent is how the DNS service returns a content: TXT quoted (a value longer than a TXT
// string split into several quoted strings, "a" "b"), host targets lowercase without trailing dot.
func recordContent(recordType, content string) string {
	switch {
	case recordType == "TXT" && (len(content) < 2 || !strings.HasPrefix(content, `"`) || !strings.HasSuffix(content, `"`)):
		var parts []string
		for len(content) > maxTXTString {
			parts = append(parts, `"`+content[:maxTXTString]+`"`)
			content = content[maxTXTString:]
		}
		return strings.Join(append(parts, `"`+content+`"`), " ")
	case slices.Contains(hostTargetTypes, recordType):
		return strings.TrimSuffix(strings.ToLower(content), ".")
	case recordType == "A" || recordType == "AAAA":
		if address, err := netip.ParseAddr(content); err == nil {
			return address.String()
		}
		return content
	default:
		return content
	}
}

// recordFields validates a record body, filling defaults from current (the record being updated,
// or nil on create). It answers the validation error itself and returns ok=false then.
func recordFields(w http.ResponseWriter, body, current Object) (Object, bool) {
	fields := Object{}
	for key, value := range current {
		fields[key] = value
	}
	errs := fieldErrors{}
	required := current == nil
	for _, key := range []string{"record_type", "name", "content"} {
		if _, present := body[key]; present || required {
			if value := errs.requireString(body, key, 0); value != "" {
				fields[key] = value
			}
		}
	}
	recordType := strings.ToUpper(fields.String("record_type"))
	if recordType != "" && !slices.Contains(recordTypes, recordType) {
		errs.add("record_type", fmt.Sprintf("%q is not a valid choice.", recordType))
	}
	fields["record_type"] = recordType
	if raw, present := body["ttl"]; present {
		ttl, isNumber := raw.(float64)
		if !isNumber || (ttl != 1 && (ttl < 60 || ttl > 86400)) {
			errs.add("ttl", "TTL must be 1 (automatic) or between 60 and 86400 seconds.")
		}
		fields["ttl"] = raw
	} else if current == nil {
		fields["ttl"] = 3600
	}
	if raw, present := body["priority"]; present {
		if priority, isNumber := raw.(float64); raw != nil && (!isNumber || priority < 0 || priority > 65535) {
			errs.add("priority", "Ensure this value is between 0 and 65535.")
		}
		if raw != nil || current == nil {
			fields["priority"] = raw
		}
	} else if current == nil {
		fields["priority"] = nil
	}
	if raw, present := body["proxied"]; present {
		fields["proxied"] = raw == true
	} else if current == nil {
		fields["proxied"] = false
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return nil, false
	}
	if fields["proxied"] == true {
		if !slices.Contains(proxiableTypes, recordType) {
			writeFieldErrors(w, map[string][]string{"proxied": {"Only A, AAAA and CNAME records can be proxied."}})
			return nil, false
		}
		fields["ttl"] = 1
	}
	return fields, true
}

func (s *Server) createDNSRecord(w http.ResponseWriter, r *http.Request, scope Scope, zone Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	fields, ok := recordFields(w, body, nil)
	if !ok {
		return
	}
	zoneName := zone.String("zone_name")
	fields["name"] = recordFQDN(fields.String("name"), zoneName)
	fields["content"] = recordContent(fields.String("record_type"), fields.String("content"))
	for _, existing := range s.list(KindDNSRecord, scope, zone.ID()) {
		if existing["record_type"] == fields["record_type"] && existing["name"] == fields["name"] &&
			existing["content"] == fields["content"] {
			writeError(w, http.StatusConflict, "An identical record already exists.", "dns_record_conflict")
			return
		}
	}
	fields["instance"] = nil
	record := s.insert(KindDNSRecord, scope, zone.ID(), fields)
	s.countRecords(scope, zone.ID())
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) updateDNSRecord(w http.ResponseWriter, r *http.Request, scope Scope, zone Object) {
	id := r.PathValue("record_id")
	current, ok := s.lookupQuiet(KindDNSRecord, scope, zone.ID(), id)
	if !ok {
		writeNotFound(w)
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	fields, ok := recordFields(w, body, current)
	if !ok {
		return
	}
	fields["name"] = recordFQDN(fields.String("name"), zone.String("zone_name"))
	fields["content"] = recordContent(fields.String("record_type"), fields.String("content"))
	updated, _ := s.update(KindDNSRecord, scope, zone.ID(), id, func(o Object) {
		for key, value := range fields {
			o[key] = value
		}
	})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) dnssecOf(scope Scope, zoneID string) Object {
	if state, ok := s.lookup(KindDNSSEC, scope, zoneID, zoneID); ok {
		return state
	}
	return s.SeedDNSSEC(scope, zoneID, nil)
}

func (s *Server) updateDNSSEC(w http.ResponseWriter, r *http.Request, scope Scope, zone Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	enabled, isBool := body["enabled"].(bool)
	if !isBool {
		writeFieldErrors(w, map[string][]string{"enabled": {"This field is required."}})
		return
	}
	zoneID := zone.ID()
	s.dnssecOf(scope, zoneID)
	var settled func(Object)
	updated, _ := s.update(KindDNSSEC, scope, zoneID, zoneID, func(o Object) {
		switch {
		case enabled && o["status"] != "active":
			o["status"] = "pending"
			o["ds_record"] = zone.String("zone_name") + ". 3600 IN DS 2371 13 2 C0FFEE"
			o["key_tag"] = 2371
			o["algorithm"] = "13"
			o["digest_type"] = "2"
			o["digest"] = "C0FFEE"
			o["public_key"] = "mdsswUyr3DPW132mOi8V9xESWE8jTo0d"
			o["flags"] = 257
			settled = func(o Object) {
				if o["status"] == "pending" {
					o["status"] = "active"
				}
			}
		case !enabled && o["status"] != "disabled":
			o["status"] = "pending_disabled"
			settled = func(o Object) {
				if o["status"] == "pending_disabled" {
					for _, key := range []string{"ds_record", "key_tag", "algorithm", "digest_type", "digest", "public_key", "flags"} {
						o[key] = nil
					}
					o["status"] = "disabled"
				}
			}
		}
	})
	if settled != nil {
		s.afterPolls(KindDNSSEC, zoneID, s.taskPolls, settled)
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) updateDomainNameservers(w http.ResponseWriter, r *http.Request, scope Scope, domain Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	raw, isList := body["nameservers"].([]any)
	if !isList || len(raw) < 2 || len(raw) > 5 {
		writeFieldErrors(w, map[string][]string{"nameservers": {"Ensure this field has between 2 and 5 elements."}})
		return
	}
	nameservers := make([]any, 0, len(raw))
	for _, value := range raw {
		name, isString := value.(string)
		if !isString || name == "" {
			writeFieldErrors(w, map[string][]string{"nameservers": {"Not a valid string."}})
			return
		}
		nameservers = append(nameservers, name)
	}
	domainID := domain.ID()
	task := s.newDomainTask(scope, domainID, TaskUpdateNameservers, func() {
		s.update(KindDomain, scope, "", domainID, func(o Object) { o["nameservers"] = nameservers })
	})
	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) setDomainAutoRenew(w http.ResponseWriter, r *http.Request, scope Scope, domain Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	enabled, isBool := body["auto_renew"].(bool)
	if !isBool {
		writeFieldErrors(w, map[string][]string{"auto_renew": {"This field is required."}})
		return
	}
	updated, _ := s.update(KindDomain, scope, "", domain.ID(), func(o Object) { o["auto_renew"] = enabled })
	writeJSON(w, http.StatusOK, updated)
}

// domainFlagTask answers an action that sets a boolean of the domain once its task completes.
func (s *Server) domainFlagTask(taskType, field string, value bool) func(http.ResponseWriter, *http.Request, Scope, Object) {
	return func(w http.ResponseWriter, _ *http.Request, scope Scope, domain Object) {
		domainID := domain.ID()
		task := s.newDomainTask(scope, domainID, taskType, func() {
			s.update(KindDomain, scope, "", domainID, func(o Object) { o[field] = value })
		})
		writeJSON(w, http.StatusAccepted, task)
	}
}
