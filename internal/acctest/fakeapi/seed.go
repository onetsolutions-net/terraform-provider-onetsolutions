package fakeapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"golang.org/x/crypto/ssh"
)

//go:embed testdata/*.json
var fixtures embed.FS

// The catalog fixtures are pages captured from the real, anonymous catalog endpoints.
var catalogFixtures = map[Kind]string{
	KindSize:         "compute_sizes.json",
	KindPublicImage:  "compute_images_public.json",
	KindControlPanel: "compute_control_panels.json",
	KindHostingPlan:  "hosting_plans.json",
}

func (s *Server) seedCatalogue() {
	for kind, file := range catalogFixtures {
		raw, err := fixtures.ReadFile(path.Join("testdata", file))
		if err != nil {
			s.t.Fatalf("fakeapi: reading %s: %v", file, err)
		}
		var page struct {
			Results []Object `json:"results"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			s.t.Fatalf("fakeapi: decoding %s: %v", file, err)
		}
		for _, item := range page.Results {
			s.insert(kind, Scope{}, "", item)
		}
	}
}

// First returns the first object of a global kind (a catalog entry), failing the test when
// there is none.
func (s *Server) First(kind Kind) Object {
	s.t.Helper()
	items := s.list(kind, Scope{}, "")
	if len(items) == 0 {
		s.t.Fatalf("fakeapi: no %s seeded", kind)
	}
	return items[0]
}

func merge(base, overrides Object) Object {
	for key, value := range overrides {
		base[key] = value
	}
	return base
}

func (s *Server) nextName(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("%s-%d", prefix, s.store.seq+1)
}

// SeedOrganization adds an organization the API key's user belongs to.
func (s *Server) SeedOrganization(name string) Object {
	return s.insert(KindOrganization, Scope{}, "", Object{
		"name":                   name,
		"organization_type":      "professional",
		"support_plan":           "basic",
		"user_quota":             10,
		"project_quota":          10,
		"domain_quota":           10,
		"logo":                   "",
		"company_name":           name + " SAS",
		"address_1":              "1 rue de la Paix",
		"address_2":              "",
		"city":                   "Paris",
		"state":                  "",
		"postcode":               "75002",
		"country":                "FR",
		"phone":                  "",
		"vat_number":             "",
		"is_active":              true,
		"missing_profile_fields": []any{},
	})
}

// SeedProject adds a project to an organization.
func (s *Server) SeedProject(organizationID, name string) Object {
	return s.insert(KindProject, Scope{OrganizationID: organizationID}, "", Object{
		"name":         name,
		"domain_quota": 10,
		"logo":         "",
	})
}

// NewPublicKey returns a fresh, valid OpenSSH ed25519 public key and its SHA256 fingerprint as
// the API computes it.
func NewPublicKey(comment string) (publicKey, fingerprint string) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		panic(err)
	}
	publicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	if comment != "" {
		publicKey += " " + comment
	}
	return publicKey, ssh.FingerprintSHA256(key)
}

// SeedSSHKey adds an SSH key to a project.
func (s *Server) SeedSSHKey(scope Scope, overrides Object) Object {
	name := s.nextName("seeded-key")
	key, fingerprint := NewPublicKey(name)
	return s.insert(KindSSHKey, scope, "", merge(Object{
		"name":        name,
		"public_key":  key,
		"fingerprint": fingerprint,
		"is_active":   true,
	}, overrides))
}

// SeedVPC adds a VPC to a project.
func (s *Server) SeedVPC(scope Scope, overrides Object) Object {
	return s.insert(KindVPC, scope, "", merge(Object{
		"name":       s.nextName("seeded-vpc"),
		"cidr_block": "10.10.0.0/16",
		"is_active":  true,
	}, overrides))
}

// SeedInstance adds an active, installed, running instance with a primary IPv4 address. The
// override "vpc_id" (a VPC of the same project) is turned into the "vpc" object the API serves;
// "order_id" is null unless overridden, and absent under WithoutOrderLink.
func (s *Server) SeedInstance(scope Scope, overrides Object) Object {
	overrides = clone(overrides)
	var vpc any
	if vpcID, ok := overrides["vpc_id"].(string); ok {
		delete(overrides, "vpc_id")
		if found, exists := s.lookupQuiet(KindVPC, scope, "", vpcID); exists {
			vpc = found
		}
	}
	image := s.First(KindPublicImage)
	instance := s.insert(KindInstance, scope, "", merge(Object{
		"organization_id":           scope.OrganizationID,
		"project_id":                scope.ProjectID,
		"order_id":                  nil,
		"name":                      s.nextName("seeded-instance"),
		"status":                    "active",
		"size":                      s.First(KindSize),
		"vpc":                       vpc,
		"image":                     image,
		"os_name":                   image["os_name"],
		"password_reset_enabled":    true,
		"rescue_expires_at":         nil,
		"primary_ipv4":              nil,
		"primary_ipv6":              nil,
		"ip_addresses":              []any{},
		"volume_config_id":          nil,
		"volumes":                   []any{},
		"billing_cycle":             "monthly",
		"recurring_amount":          "7.99",
		"billing_start_date":        now(),
		"billing_end_date":          nil,
		"next_billing_date":         nil,
		"cancellation_requested_at": nil,
		"cancellation_reason":       "",
		"cancellation_reason_code":  "other",
		"bandwidth_used_gb":         "0.00",
		"tags":                      []any{},
		"is_active":                 true,
		"is_installed":              true,
		"snapshot_enabled":          false,
		"snapshot_billing":          Object{"included": 0, "unit_price": nil, "currency": "EUR", "metered": true},
		"backup_enabled":            false,
		"control_panel":             nil,
		"active_addons":             []any{},
		"can_run_windows":           false,
		"gpu_capable":               false,
		"state_sync":                Object{"vm_status": "running", "last_synced_at": now()},
		"agent_info":                Object{},
		"locked":                    false,
		"lock_reason":               nil,
	}, overrides))

	instanceID := instance.ID()
	address := s.SeedIPAddress(scope, instanceID, Object{"is_primary": true})
	updated, _ := s.update(KindInstance, scope, "", instanceID, func(o Object) {
		if o["primary_ipv4"] == nil {
			o["primary_ipv4"] = address["address"]
		}
		o["ip_addresses"] = s.ipAddressesOfLocked(scope, instanceID)
		if s.withoutOrderLink {
			delete(o, "order_id")
		}
	})
	return updated
}

func (s *Server) ipAddressesOfLocked(scope Scope, instanceID string) []Object {
	return s.store.list(KindIPAddress, scope, instanceID)
}

// SeedIPAddress adds an IPv4 address to an instance.
func (s *Server) SeedIPAddress(scope Scope, instanceID string, overrides Object) Object {
	s.mu.Lock()
	host := s.store.seq%250 + 2
	s.mu.Unlock()
	return s.insert(KindIPAddress, scope, instanceID, merge(Object{
		"address":       fmt.Sprintf("203.0.113.%d", host),
		"ip_type":       "ipv4",
		"interface":     0,
		"netmask":       "255.255.255.0",
		"gateway":       "203.0.113.1",
		"network_range": "203.0.113.0/24",
		"mac_address":   "bc:24:11:00:00:01",
		"is_primary":    false,
		"reverse_dns":   "",
	}, overrides))
}

// SeedFirewallRule adds a customer firewall rule to an instance.
func (s *Server) SeedFirewallRule(scope Scope, instanceID string, overrides Object) Object {
	return s.insert(KindFirewallRule, scope, instanceID, merge(Object{
		"instance":         instanceID,
		"name":             s.nextName("seeded-rule"),
		"description":      "",
		"direction":        "inbound",
		"action":           "accept",
		"protocol":         "tcp",
		"destination_port": "22",
		"source_cidr":      "0.0.0.0/0",
		"source_label":     nil,
		"destination_cidr": "",
		"priority":         100,
		"is_enabled":       true,
	}, overrides))
}

// Prefixes of the firewall rules One Click Apps manages, which the provider never touches.
const (
	ManagedRulePrefix       = "One Click Apps · "
	LegacyManagedRulePrefix = "one-click:"
	ControlPlaneLabel       = "Control Plane"
	// ControlPlaneEgressCIDR is the control plane's egress address: the API labels a rule from it
	// with ControlPlaneLabel in source_label.
	ControlPlaneEgressCIDR = "198.51.100.10/32"
)

// SeedPlatformFirewallRules adds the rules the platform manages on an instance: one restricted
// to the control plane (source_label set) and two One Click Apps rules (current and legacy
// prefix).
func (s *Server) SeedPlatformFirewallRules(scope Scope, instanceID string) []Object {
	return []Object{
		s.SeedFirewallRule(scope, instanceID, Object{
			"name":             ManagedRulePrefix + "Allow Control Plane (8443)",
			"destination_port": "8443",
			"source_cidr":      ControlPlaneEgressCIDR,
			"source_label":     ControlPlaneLabel,
			"priority":         10,
		}),
		s.SeedFirewallRule(scope, instanceID, Object{
			"name":             ManagedRulePrefix + "Public HTTPS (443)",
			"destination_port": "443",
			"priority":         20,
		}),
		s.SeedFirewallRule(scope, instanceID, Object{
			"name":             LegacyManagedRulePrefix + "http",
			"destination_port": "80",
			"priority":         30,
		}),
	}
}

// SeedSnapshot adds a ready snapshot to an instance.
func (s *Server) SeedSnapshot(scope Scope, instanceID string, overrides Object) Object {
	return s.insert(KindSnapshot, scope, instanceID, merge(Object{
		"instance_id": instanceID,
		"name":        s.nextName("seeded-snapshot"),
		"description": "",
		"size_gb":     "1.50",
		"status":      "present",
	}, overrides))
}

// SeedPrivateImage adds a private image to a project.
func (s *Server) SeedPrivateImage(scope Scope, overrides Object) Object {
	name := s.nextName("seeded-image")
	return s.insert(KindPrivateImage, scope, "", merge(Object{
		"organization_id":        scope.OrganizationID,
		"name":                   name,
		"slug":                   name,
		"description":            "",
		"visibility":             "private",
		"os_name":                "Debian",
		"os_version":             "13",
		"os_architecture":        "x86_64",
		"language":               "",
		"is_active":              true,
		"is_featured":            false,
		"is_windows":             false,
		"requires_license_addon": false,
		"min_disk_gb":            10,
		"min_ram_mb":             512,
	}, overrides))
}

// SeedDNSZone adds a DNS zone to a project.
func (s *Server) SeedDNSZone(scope Scope, overrides Object) Object {
	return s.insert(KindDNSZone, scope, "", merge(Object{
		"zone_name":              s.nextName("seeded-zone") + ".example",
		"mode":                   "onetsolutions",
		"nameservers":            []any{"ns1.onetsolutions.net", "ns2.onetsolutions.net"},
		"connected_account_name": nil,
		"domain_id":              nil,
		"is_active":              true,
		"is_verified":            true,
		"record_count":           0,
		"records":                []any{},
	}, overrides))
}

// SeedDNSRecord adds a record to a zone.
func (s *Server) SeedDNSRecord(scope Scope, zoneID string, overrides Object) Object {
	return s.insert(KindDNSRecord, scope, zoneID, merge(Object{
		"record_type": "A",
		"name":        s.nextName("seeded"),
		"content":     "203.0.113.10",
		"ttl":         3600,
		"priority":    nil,
		"proxied":     false,
		"instance":    nil,
	}, overrides))
}

// SeedDNSSEC sets the DNSSEC state of a zone (disabled unless overridden).
func (s *Server) SeedDNSSEC(scope Scope, zoneID string, overrides Object) Object {
	return s.insert(KindDNSSEC, scope, zoneID, merge(Object{
		"id":                        zoneID,
		"status":                    "disabled",
		"ds_record":                 nil,
		"key_tag":                   nil,
		"algorithm":                 nil,
		"digest_type":               nil,
		"digest":                    nil,
		"public_key":                nil,
		"flags":                     nil,
		"domain_registered_with_us": false,
		"automatic_ds_publication":  false,
	}, overrides))
}

// SeedDomain adds an active registered domain to a project.
func (s *Server) SeedDomain(scope Scope, overrides Object) Object {
	sld := s.nextName("seeded-domain")
	return s.insert(KindDomain, scope, "", merge(Object{
		"name":              sld + ".fr",
		"sld":               sld,
		"tld":               "fr",
		"status":            "active",
		"registration_date": "2026-01-01",
		"expiration_date":   "2027-01-01",
		"auto_renew":        true,
		"whois_privacy":     false,
		"is_locked":         true,
		"nameservers":       []any{"ns1.onetsolutions.net", "ns2.onetsolutions.net"},
		"billing_cycle":     1,
		"next_billing_date": "2027-01-01",
		"recurring_amount":  "9.99",
		"dns_zone":          nil,
		"web_hosting":       nil,
		"website_preview":   nil,
		"is_active":         true,
	}, overrides))
}

// SeedInstanceTask queues a task on an instance; it completes after the configured polls.
func (s *Server) SeedInstanceTask(scope Scope, instanceID, taskType string) Object {
	return s.newInstanceTask(scope, instanceID, taskType, Object{}, nil)
}

// SeedDomainTask queues a task on a domain; it completes after the configured polls.
func (s *Server) SeedDomainTask(scope Scope, domainID, taskType string) Object {
	return s.newDomainTask(scope, domainID, taskType, nil)
}

// SeedWebHosting adds an active web hosting to a project, on the first catalog plan.
func (s *Server) SeedWebHosting(scope Scope, overrides Object) Object {
	return s.insert(KindWebHosting, scope, "", merge(Object{
		"organization_id":    scope.OrganizationID,
		"project_id":         scope.ProjectID,
		"project_name":       "Default",
		"domain":             s.nextName("seeded-site") + ".fr",
		"username":           "seeded",
		"status":             "active",
		"plan":               s.First(KindHostingPlan),
		"server":             Object{"hostname": "web1.onetsolutions.net"},
		"disk_usage_mb":      0,
		"disk_limit_mb":      50000,
		"bandwidth_usage_mb": 0,
		"bandwidth_limit_mb": 0,
		"dedicated_ip":       nil,
		"ns1":                "ns1.onetsolutions.net",
		"ns2":                "ns2.onetsolutions.net",
		"billing_cycle":      "monthly",
		"price":              "5.99",
		"next_billing_date":  nil,
		"website_preview":    nil,
		"is_active":          true,
	}, overrides))
}
