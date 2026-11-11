package fakeapi

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// KindTLD is an extension of the domain catalog (global).
const KindTLD Kind = "tld"

const (
	tldsTemplate         = "/v1/domains/tlds"
	availabilityTemplate = "/v1/domains/availability/check"
	domainOrderTemplate  = domainsTemplate + "/order"
)

// TakenPrefix starts every name the fake reports as registered by someone else
// ("taken-shop.com").
const TakenPrefix = "taken"

// domainOrders is the state of the domain registrations the fake has taken.
type domainOrders struct {
	unpaid   bool
	failNext bool
	// unpaidDomains maps a domain awaiting payment to its order's local id.
	unpaidDomains map[string]string
}

func (s *Server) domainOrderState() *domainOrders {
	if s.domainOrders == nil {
		s.domainOrders = &domainOrders{unpaidDomains: map[string]string{}}
	}
	return s.domainOrders
}

// WithUnpaidDomainOrders leaves every domain order unpaid, as when the account balance does not
// cover it: the answer carries amount_due > 0 and the domain stays pending until PayDomainOrder.
func WithUnpaidDomainOrders() Option {
	return func(s *Server) { s.domainOrderState().unpaid = true }
}

// FailNextDomainRegistration makes the next ordered domain end failed instead of active.
func (s *Server) FailNextDomainRegistration() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.domainOrderState().failNext = true
}

// PayDomainOrder pays the order of a domain WithUnpaidDomainOrders left pending: it is then
// registered after a few reads.
func (s *Server) PayDomainOrder(domainID string) {
	s.mu.Lock()
	_, ok := s.domainOrderState().unpaidDomains[domainID]
	delete(s.domainOrderState().unpaidDomains, domainID)
	s.mu.Unlock()
	if !ok {
		s.t.Fatalf("fakeapi: domain %s has no unpaid order", domainID)
	}
	s.scheduleRegistration(domainID, false)
}

// SeedTLD adds an extension to the domain catalog, with a one- and a two-year price.
func (s *Server) SeedTLD(tld string, overrides Object) Object {
	return s.insert(KindTLD, Scope{}, "", merge(Object{
		"tld":                       tld,
		"display_name":              "." + tld,
		"category":                  "generic",
		"is_active":                 true,
		"is_registration_available": true,
		"is_transfer_available":     true,
		"whois_privacy_available":   true,
		"whois_privacy_price":       "0.00",
		"transfer_lock_available":   true,
		"min_registration_years":    1,
		"max_registration_years":    10,
		"pricing": []any{
			Object{"id": uuid.NewString(), "years": 1, "registration_price": "11.99", "renewal_price": "13.99", "transfer_price": "11.99"},
			Object{"id": uuid.NewString(), "years": 2, "registration_price": "25.98", "renewal_price": "27.98", "transfer_price": "25.98"},
		},
	}, overrides))
}

func (s *Server) seedTLDs() {
	s.SeedTLD("com", nil)
	s.SeedTLD("fr", Object{"category": "country", "display_name": ".fr", "pricing": []any{
		Object{"id": uuid.NewString(), "years": 1, "registration_price": "7.99", "renewal_price": "9.99", "transfer_price": "7.99"},
	}})
	s.SeedTLD("io", Object{"is_registration_available": false})
}

// registerDomainOrders serves the domain catalog, availability checks (both anonymous) and the
// registration orders of a project. A paid order registers its domain after a few reads.
func registerDomainOrders(s *Server) {
	s.domainOrderState()
	s.seedTLDs()

	s.handle(http.MethodGet, tldsTemplate, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		var items []Object
		for _, tld := range s.list(KindTLD, Scope{}, "") {
			if want := query.Get("tld"); want != "" && !strings.EqualFold(tld.String("tld"), want) {
				continue
			}
			if want := query.Get("category"); want != "" && !strings.EqualFold(tld.String("category"), want) {
				continue
			}
			if want := query.Get("is_registration_available"); want != "" {
				if flag, err := strconv.ParseBool(want); err == nil && tld["is_registration_available"] != flag {
					continue
				}
			}
			items = append(items, tld)
		}
		writePage(w, r, items)
	}, anonymous)

	s.handle(http.MethodGet, availabilityTemplate, func(w http.ResponseWriter, r *http.Request) {
		name := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
		if !domainNamePattern.MatchString(name) {
			writeFieldErrors(w, fieldErrors{"domain": {"Invalid domain name format"}})
			return
		}
		writeJSON(w, http.StatusOK, s.availability(name))
	}, anonymous)

	s.handle(http.MethodPost, domainOrderTemplate, s.withScope(s.orderDomain))
}

var domainNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func (s *Server) tldOf(name string) (Object, bool) {
	var best Object
	for _, tld := range s.list(KindTLD, Scope{}, "") {
		suffix := tld.String("tld")
		if strings.HasSuffix(name, "."+suffix) && len(suffix) > len(best.String("tld")) {
			best = tld
		}
	}
	return best, best != nil
}

// oneYear returns the one-year prices of a catalog extension.
func oneYear(tld Object) map[string]any {
	for _, raw := range tld.List("pricing") {
		if pricing, ok := raw.(map[string]any); ok && pricing["years"] == float64(1) {
			return pricing
		}
	}
	return nil
}

func firstYearPrice(tld Object) any { return oneYear(tld)["registration_price"] }

func renewalPrice(tld Object) any { return oneYear(tld)["renewal_price"] }

// registeredDomain returns the domain of any organization holding name, as the registry sees it.
func (s *Server) registeredDomain(name string) (Object, bool) {
	for _, domain := range s.All(KindDomain) {
		if domain.String("name") == name && !reorderable(domain.String("status")) {
			return domain, true
		}
	}
	return nil, false
}

func reorderable(status string) bool {
	return status == "failed" || status == "deleted" || status == "pending_delete"
}

func (s *Server) availability(name string) Object {
	tld, known := s.tldOf(name)
	tldName := name[strings.LastIndex(name, ".")+1:]
	if known {
		tldName = tld.String("tld")
	}
	_, held := s.registeredDomain(name)
	available := !held && !strings.HasPrefix(name, TakenPrefix)
	var price any
	suggestions := []any{}
	if available && known {
		price = firstYearPrice(tld)
	}
	if !available {
		sld := strings.TrimSuffix(name, "."+tldName)
		for _, candidate := range s.list(KindTLD, Scope{}, "") {
			if candidate.String("tld") == tldName || candidate["is_registration_available"] != true {
				continue
			}
			suggestions = append(suggestions, Object{
				"domain_name": sld + "." + candidate.String("tld"), "tld": candidate.String("tld"),
				"available": true, "price": firstYearPrice(candidate),
			})
		}
	}
	return Object{"domain_name": name, "available": available, "tld": tldName, "price": price, "suggestions": suggestions}
}

// orderDomain answers create_domain_order for order_type register as the API does: the domain
// exists from the answer on, pending; it registers once paid.
func (s *Server) orderDomain(w http.ResponseWriter, r *http.Request, scope Scope) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	name := strings.ToLower(strings.TrimSpace(errs.requireString(body, "domain_name", 253)))
	orderType, _ := stringField(body, "order_type")
	if orderType != "register" && orderType != "transfer" && orderType != "external" {
		errs.add("order_type", `"`+orderType+`" is not a valid choice.`)
	}
	if name != "" && !domainNamePattern.MatchString(name) {
		errs.add("domain_name", "Invalid domain name format")
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	if orderType != "register" {
		writeError(w, http.StatusNotImplemented, "fakeapi: only registrations are served", "not_implemented")
		return
	}
	failed := Object{"error": "Failed to create domain order. Please try again later."}
	tld, known := s.tldOf(name)
	if !known || tld["is_registration_available"] != true {
		writeJSON(w, http.StatusBadRequest, failed)
		return
	}
	availability := s.availability(name)
	if availability["available"] != true {
		if held, ok := s.registeredDomain(name); ok && held.String("status") == "pending" && s.awaitingPayment(held.ID()) {
			writeJSON(w, http.StatusConflict, Object{
				"error": "This domain is already on an order awaiting payment.", "domain_name": name, "invoice_id": "4242",
			})
			return
		}
		if held, ok := s.registeredDomain(name); ok && s.sameOrganization(held, scope) {
			writeJSON(w, http.StatusBadRequest, failed)
			return
		}
		writeJSON(w, http.StatusConflict, Object{
			"error": "Domain not available", "domain_name": name, "available": false, "suggestions": availability["suggestions"],
		})
		return
	}

	domain := s.pendingDomain(scope, name, tld)
	listed, _ := firstYearPrice(tld).(string)
	price, _ := strconv.ParseFloat(listed, 64)
	localOrderID := uuid.NewString()
	order := Object{
		"order_id": 9001, "order_number": "ORD-" + localOrderID[:8], "invoice_id": 4242, "local_order_id": localOrderID,
		"total": price, "amount_due": 0.0, "credit_applied": price, "status": "Active",
		"invoice_payment_url": nil, "domain_id": domain.ID(), "domain_status": "pending",
	}
	s.mu.Lock()
	state := s.domainOrderState()
	unpaid, fail := state.unpaid, state.failNext
	state.failNext = false
	if unpaid {
		state.unpaidDomains[domain.ID()] = localOrderID
	}
	s.mu.Unlock()
	if unpaid {
		order["amount_due"], order["credit_applied"], order["status"] = price, 0.0, "Pending"
		order["invoice_payment_url"] = "https://console.example.test/billing/invoices/4242"
	} else {
		s.scheduleRegistration(domain.ID(), fail)
	}
	writeJSON(w, http.StatusCreated, order)
}

func (s *Server) awaitingPayment(domainID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.domainOrderState().unpaidDomains[domainID]
	return ok
}

func (s *Server) sameOrganization(domain Object, scope Scope) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.store.byID(KindDomain, domain.ID())
	return ok && rec.scope.OrganizationID == scope.OrganizationID
}

// pendingDomain creates the pending domain of an order, or reuses the row a failed order of the
// same organization left behind, as the API does.
func (s *Server) pendingDomain(scope Scope, name string, tld Object) Object {
	fields := Object{
		"name":              name,
		"sld":               strings.TrimSuffix(name, "."+tld.String("tld")),
		"tld":               tld.String("tld"),
		"tld_info":          tld,
		"status":            "pending",
		"registration_date": nil,
		"expiration_date":   nil,
		"auto_renew":        true,
		"whois_privacy":     false,
		"is_locked":         false,
		"nameservers":       []any{"ns1.onetsolutions.net", "ns2.onetsolutions.net"},
		"billing_cycle":     1,
		"next_billing_date": nil,
		"recurring_amount":  renewalPrice(tld),
		"dns_zone":          nil,
		"web_hosting":       nil,
		"website_preview":   nil,
		"is_active":         true,
	}
	for _, domain := range s.All(KindDomain) {
		if domain.String("name") != name || !s.sameOrganization(domain, scope) {
			continue
		}
		s.mu.Lock()
		rec, _ := s.store.byID(KindDomain, domain.ID())
		rec.scope = scope
		for key, value := range fields {
			rec.data[key] = value
		}
		reused := clone(rec.data)
		s.mu.Unlock()
		return reused
	}
	return s.insert(KindDomain, scope, "", fields)
}

// scheduleRegistration moves a paid domain to registering, then to active (or failed) a few
// reads later.
func (s *Server) scheduleRegistration(domainID string, fail bool) {
	s.afterPolls(KindDomain, domainID, s.taskPolls, func(o Object) { o["status"] = "registering" })
	s.afterPolls(KindDomain, domainID, 2*s.taskPolls, func(o Object) {
		if fail {
			o["status"] = "failed"
			return
		}
		today := time.Now().UTC()
		expires := today.AddDate(1, 0, 0).Format(time.DateOnly)
		o["status"] = "active"
		o["registration_date"] = today.Format(time.DateOnly)
		o["expiration_date"] = expires
		o["next_billing_date"] = expires
		o["is_locked"] = true
	})
}
