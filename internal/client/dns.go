package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// DNSZone is the DNSZone schema (and DNSZoneList, which lacks records).
type DNSZone struct {
	ID          string          `json:"id"`
	ZoneName    string          `json:"zone_name"`
	Mode        string          `json:"mode"`
	Nameservers json.RawMessage `json:"nameservers"`
	DomainID    *string         `json:"domain_id"`
	IsActive    bool            `json:"is_active"`
	IsVerified  bool            `json:"is_verified"`
	RecordCount int64           `json:"record_count"`
	Created     string          `json:"created"`
}

// NameserverList returns the zone's name servers; the field is an untyped JSON list, null until
// the zone is provisioned.
func (z *DNSZone) NameserverList() []string { return stringList(z.Nameservers) }

// DNSZoneCreate is the body of create_dns_zone.
type DNSZoneCreate struct {
	ZoneName string `json:"zone_name"`
	Mode     string `json:"mode,omitempty"`
}

// DNSRecord is the DNSRecord schema.
type DNSRecord struct {
	ID         string `json:"id"`
	RecordType string `json:"record_type"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TTL        int64  `json:"ttl"`
	Priority   *int64 `json:"priority"`
	Proxied    bool   `json:"proxied"`
	Created    string `json:"created"`
}

// DNSRecordWrite is the body of create_dns_record and update_dns_record. A nil TTL lets the API
// choose (3600, or automatic on a proxied record).
type DNSRecordWrite struct {
	RecordType string `json:"record_type"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TTL        *int64 `json:"ttl,omitempty"`
	Priority   *int64 `json:"priority"`
	Proxied    bool   `json:"proxied"`
}

// DNSSEC is the DNSZoneDNSSEC schema.
type DNSSEC struct {
	Status                 string  `json:"status"`
	DSRecord               *string `json:"ds_record"`
	KeyTag                 *int64  `json:"key_tag"`
	Algorithm              *string `json:"algorithm"`
	DigestType             *string `json:"digest_type"`
	Digest                 *string `json:"digest"`
	PublicKey              *string `json:"public_key"`
	Flags                  *int64  `json:"flags"`
	DomainRegisteredWithUs bool    `json:"domain_registered_with_us"`
	AutomaticDSPublication bool    `json:"automatic_ds_publication"`
}

// DNSSEC statuses.
const (
	DNSSECActive          = "active"
	DNSSECPending         = "pending"
	DNSSECDisabled        = "disabled"
	DNSSECPendingDisabled = "pending_disabled"
	DNSSECError           = "error"
)

// Domain is the Domain schema, reduced to what the provider reads.
type Domain struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Status           string          `json:"status"`
	RegistrationDate *string         `json:"registration_date"`
	ExpirationDate   *string         `json:"expiration_date"`
	AutoRenew        bool            `json:"auto_renew"`
	WhoisPrivacy     bool            `json:"whois_privacy"`
	IsLocked         bool            `json:"is_locked"`
	Nameservers      json.RawMessage `json:"nameservers"`
	NextBillingDate  *string         `json:"next_billing_date"`
	RecurringAmount  *string         `json:"recurring_amount"`
	DNSZone          *DNSZone        `json:"dns_zone"`
	IsActive         bool            `json:"is_active"`
}

// NameserverList returns the domain's name servers; the field is an untyped JSON list.
func (d *Domain) NameserverList() []string { return stringList(d.Nameservers) }

// DomainSummary is the DomainList schema, what the list endpoint returns.
type DomainSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
}

const (
	dnsZonesPath = "/v1/organizations/{organization_id}/projects/{project_id}/dns-zones"
	domainsPath  = "/v1/organizations/{organization_id}/projects/{project_id}/domains"
)

var (
	routeListDNSZones  = newRoute(http.MethodGet, dnsZonesPath)
	routeCreateDNSZone = newRoute(http.MethodPost, dnsZonesPath)
	routeGetDNSZone    = newRoute(http.MethodGet, dnsZonesPath+"/{id}")
	routeDeleteDNSZone = newRoute(http.MethodDelete, dnsZonesPath+"/{id}")

	routeCreateDNSRecord = newRoute(http.MethodPost, dnsZonesPath+"/{id}/records")
	routeGetDNSRecord    = newRoute(http.MethodGet, dnsZonesPath+"/{id}/records/{record_id}")
	routeUpdateDNSRecord = newRoute(http.MethodPut, dnsZonesPath+"/{id}/records/{record_id}")
	routeDeleteDNSRecord = newRoute(http.MethodDelete, dnsZonesPath+"/{id}/records/{record_id}")

	routeGetDNSSEC    = newRoute(http.MethodGet, dnsZonesPath+"/{id}/dnssec")
	routeUpdateDNSSEC = newRoute(http.MethodPut, dnsZonesPath+"/{id}/dnssec")

	routeListDomains         = newRoute(http.MethodGet, domainsPath)
	routeGetDomain           = newRoute(http.MethodGet, domainsPath+"/{id}")
	routeDomainNameservers   = newRoute(http.MethodPut, domainsPath+"/{id}/nameservers")
	routeDomainAutoRenew     = newRoute(http.MethodPut, domainsPath+"/{id}/auto-renew")
	routeLockDomain          = newRoute(http.MethodPost, domainsPath+"/{id}/lock")
	routeUnlockDomain        = newRoute(http.MethodPost, domainsPath+"/{id}/unlock")
	routeToggleDomainPrivacy = newRoute(http.MethodPost, domainsPath+"/{id}/privacy/{action_type}")
)

// ListDNSZones lists the DNS zones of a project.
func (c *Client) ListDNSZones(ctx context.Context, scope Scope) ([]DNSZone, error) {
	return listAll[DNSZone](ctx, c, routeListDNSZones, scope.Params(), nil)
}

// CreateDNSZone creates a DNS zone; the API provisions its name servers asynchronously.
func (c *Client) CreateDNSZone(ctx context.Context, scope Scope, in DNSZoneCreate) (*DNSZone, error) {
	var zone DNSZone
	if err := c.do(ctx, routeCreateDNSZone, scope.Params(), in, &zone); err != nil {
		return nil, err
	}
	return &zone, nil
}

// GetDNSZone reads one DNS zone.
func (c *Client) GetDNSZone(ctx context.Context, scope Scope, id string) (*DNSZone, error) {
	var zone DNSZone
	if err := c.do(ctx, routeGetDNSZone, scope.Params("id", id), nil, &zone); err != nil {
		return nil, err
	}
	return &zone, nil
}

// DeleteDNSZone deletes a DNS zone and its records.
func (c *Client) DeleteDNSZone(ctx context.Context, scope Scope, id string) error {
	return c.do(ctx, routeDeleteDNSZone, scope.Params("id", id), nil, nil)
}

// CreateDNSRecord adds a record to a zone.
func (c *Client) CreateDNSRecord(ctx context.Context, scope Scope, zoneID string, in DNSRecordWrite) (*DNSRecord, error) {
	var record DNSRecord
	if err := c.do(ctx, routeCreateDNSRecord, scope.Params("id", zoneID), in, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// GetDNSRecord reads one record of a zone.
func (c *Client) GetDNSRecord(ctx context.Context, scope Scope, zoneID, id string) (*DNSRecord, error) {
	var record DNSRecord
	if err := c.do(ctx, routeGetDNSRecord, scope.Params("id", zoneID, "record_id", id), nil, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// UpdateDNSRecord replaces a record's fields.
func (c *Client) UpdateDNSRecord(ctx context.Context, scope Scope, zoneID, id string, in DNSRecordWrite) (*DNSRecord, error) {
	var record DNSRecord
	if err := c.do(ctx, routeUpdateDNSRecord, scope.Params("id", zoneID, "record_id", id), in, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// DeleteDNSRecord removes a record from a zone.
func (c *Client) DeleteDNSRecord(ctx context.Context, scope Scope, zoneID, id string) error {
	return c.do(ctx, routeDeleteDNSRecord, scope.Params("id", zoneID, "record_id", id), nil, nil)
}

// GetDNSSEC reads the DNSSEC state of a zone.
func (c *Client) GetDNSSEC(ctx context.Context, scope Scope, zoneID string) (*DNSSEC, error) {
	var state DNSSEC
	if err := c.do(ctx, routeGetDNSSEC, scope.Params("id", zoneID), nil, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// UpdateDNSSEC enables or disables DNSSEC on a zone.
func (c *Client) UpdateDNSSEC(ctx context.Context, scope Scope, zoneID string, enabled bool) (*DNSSEC, error) {
	var state DNSSEC
	body := struct {
		Enabled bool `json:"enabled"`
	}{Enabled: enabled}
	if err := c.do(ctx, routeUpdateDNSSEC, scope.Params("id", zoneID), body, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// ListDomains lists the domains of a project.
func (c *Client) ListDomains(ctx context.Context, scope Scope) ([]DomainSummary, error) {
	return listAll[DomainSummary](ctx, c, routeListDomains, scope.Params(), nil)
}

// GetDomain reads one domain.
func (c *Client) GetDomain(ctx context.Context, scope Scope, id string) (*Domain, error) {
	var domain Domain
	if err := c.do(ctx, routeGetDomain, scope.Params("id", id), nil, &domain); err != nil {
		return nil, err
	}
	return &domain, nil
}

// UpdateDomainNameservers delegates a domain to other name servers; it returns the task.
func (c *Client) UpdateDomainNameservers(ctx context.Context, scope Scope, id string, nameservers []string) (*DomainTask, error) {
	body := struct {
		Nameservers []string `json:"nameservers"`
	}{Nameservers: nameservers}
	return c.domainTask(ctx, routeDomainNameservers, scope.Params("id", id), body)
}

// SetDomainAutoRenew turns automatic renewal on or off.
func (c *Client) SetDomainAutoRenew(ctx context.Context, scope Scope, id string, enabled bool) (*Domain, error) {
	var domain Domain
	body := struct {
		AutoRenew bool `json:"auto_renew"`
	}{AutoRenew: enabled}
	if err := c.do(ctx, routeDomainAutoRenew, scope.Params("id", id), body, &domain); err != nil {
		return nil, err
	}
	return &domain, nil
}

// SetDomainLock locks or unlocks a domain at the registry; it returns the task.
func (c *Client) SetDomainLock(ctx context.Context, scope Scope, id string, locked bool) (*DomainTask, error) {
	r := routeUnlockDomain
	if locked {
		r = routeLockDomain
	}
	return c.domainTask(ctx, r, scope.Params("id", id), nil)
}

// SetDomainWhoisPrivacy enables or disables WHOIS privacy; it returns the task.
func (c *Client) SetDomainWhoisPrivacy(ctx context.Context, scope Scope, id string, enabled bool) (*DomainTask, error) {
	action := "disable"
	if enabled {
		action = "enable"
	}
	return c.domainTask(ctx, routeToggleDomainPrivacy, scope.Params("id", id, "action_type", action), nil)
}

func (c *Client) domainTask(ctx context.Context, r Route, params Params, body any) (*DomainTask, error) {
	var task DomainTask
	if err := c.do(ctx, r, params, body, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func stringList(raw json.RawMessage) []string {
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return []string{}
	}
	if values == nil {
		return []string{}
	}
	return values
}
