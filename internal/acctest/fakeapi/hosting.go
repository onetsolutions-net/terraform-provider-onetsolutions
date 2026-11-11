package fakeapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// KindWordPressSite is a WordPress site of a web hosting (parent: web hosting).
const KindWordPressSite Kind = "wordpress_site"

const webHostingTemplate = webHostingsTemplate + "/{id}"

// InstalledPHPVersions are the PHP versions the fake's hosting servers offer.
var InstalledPHPVersions = []string{"ea-php74", "ea-php81", "ea-php82", "ea-php83"}

// DefaultPHPVersion is the PHP version a domain runs until one is set.
const DefaultPHPVersion = "ea-php81"

// WordPressMinPHPVersion is the oldest PHP version a WordPress install accepts.
const WordPressMinPHPVersion = "ea-php81"

// WordPressLatestVersion is the version an install or an update leaves a WordPress site on.
const WordPressLatestVersion = "6.7.1"

var hostingBillingCycles = []string{"free", "one_time", "monthly", "quarterly", "semi_annually", "annually", "biennially", "triennially"}

var hostingDomainSources = []string{"register", "transfer", "existing", "external"}

// hostingCycleRank orders the billing cycles a plan change may lengthen.
var hostingCycleRank = map[string]int{
	"monthly": 1, "quarterly": 2, "semi_annually": 3, "annually": 4, "biennially": 5, "triennially": 6,
}

// hostingState is what the fake keeps about web hostings beyond the objects it serves.
type hostingState struct {
	unpaidOrders      bool
	unpaidPlanChanges bool
	// awaitingPayment holds the web hostings whose order is unpaid: they stay pending.
	awaitingPayment map[string]bool
	// unlinked holds the web hostings just ordered, not yet linked to their billing service: they
	// cannot be cancelled until the next read of the hosting links them.
	unlinked      map[string]bool
	planChangeDue map[string]bool
	phpVersions   map[string]map[string]string
	extraDomains  map[string][]string
	wordPressSeq  int
}

func (h *hostingState) init() {
	if h.awaitingPayment == nil {
		h.awaitingPayment = map[string]bool{}
		h.unlinked = map[string]bool{}
		h.planChangeDue = map[string]bool{}
		h.phpVersions = map[string]map[string]string{}
		h.extraDomains = map[string][]string{}
	}
}

// WithUnpaidHostingOrders leaves every web hosting order unpaid, as when the account balance
// does not cover it: the hosting is created pending and stays so until PayHostingOrder.
func WithUnpaidHostingOrders() Option {
	return func(s *Server) { s.hosting.unpaidOrders = true }
}

// WithUnpaidHostingPlanChanges leaves every plan change unpaid: the plan never changes, and a
// second change is refused with 409 like the API.
func WithUnpaidHostingPlanChanges() Option {
	return func(s *Server) { s.hosting.unpaidPlanChanges = true }
}

// registerHosting serves the web hosting orders, plan changes, cancellations, PHP versions and
// WordPress sites. The list and the read of web hostings are in catalog.go.
func registerHosting(s *Server) {
	s.handle(http.MethodPost, webHostingsTemplate+"/order", s.withScope(s.orderWebHosting))
	s.handle(http.MethodPost, webHostingTemplate+"/cancel", s.withScope(s.cancelWebHosting))
	s.handle(http.MethodPost, webHostingTemplate+"/change-plan", s.withScope(s.changeHostingPlan))
	s.handle(http.MethodGet, webHostingTemplate+"/eligible-plans", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		hosting, ok := s.lookup(KindWebHosting, scope, "", r.PathValue("id"))
		if !ok {
			writeNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, s.eligibleHostingPlans(hosting))
	}))
	s.handle(http.MethodGet, webHostingTemplate+"/php-versions", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		if _, ok := s.lookup(KindWebHosting, scope, "", r.PathValue("id")); !ok {
			writeNotFound(w)
			return
		}
		versions := make([]Object, 0, len(InstalledPHPVersions))
		for _, version := range InstalledPHPVersions {
			versions = append(versions, Object{"version": version, "display": "PHP " + version})
		}
		writeJSON(w, http.StatusOK, versions)
	}))
	s.handle(http.MethodGet, webHostingTemplate+"/domains-php-versions", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		hosting, ok := s.lookup(KindWebHosting, scope, "", r.PathValue("id"))
		if !ok {
			writeNotFound(w)
			return
		}
		domains := s.hostingDomains(scope, hosting)
		out := make([]Object, 0, len(domains))
		for _, domain := range domains {
			// Like the control panel, a domain that inherits is listed with the version it runs.
			version := s.HostingPHPVersion(hosting.ID(), domain)
			if version == "inherit" {
				version = DefaultPHPVersion
			}
			out = append(out, Object{"domain": domain, "php_version": version})
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, webHostingTemplate+"/php-version", s.withScope(s.setHostingPHPVersion))
	s.handle(http.MethodGet, webHostingTemplate+"/wordpress", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		id := r.PathValue("id")
		if _, ok := s.lookup(KindWebHosting, scope, "", id); !ok {
			writeNotFound(w)
			return
		}
		sites := s.list(KindWordPressSite, scope, id)
		out := make([]Object, 0, len(sites))
		for _, site := range sites {
			out = append(out, Object{
				"id": site["id"], "domain": site["domain"], "path": site["path"], "title": site["title"], "version": site["version"],
			})
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, webHostingTemplate+"/wordpress/install", s.withScope(s.installWordPress))
	s.handle(http.MethodPost, webHostingTemplate+"/wordpress/{instance_id}/update", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		hosting, ok := s.activeHosting(w, scope, r.PathValue("id"))
		if !ok {
			return
		}
		if _, ok := s.update(KindWordPressSite, scope, hosting.ID(), r.PathValue("instance_id"), func(o Object) {
			o["version"] = WordPressLatestVersion
		}); !ok {
			writeNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, Object{"success": true})
	}))
	s.handle(http.MethodPost, webHostingTemplate+"/wordpress/{instance_id}/delete", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		hosting, ok := s.activeHosting(w, scope, r.PathValue("id"))
		if !ok {
			return
		}
		s.deleteObject(w, KindWordPressSite, scope, hosting.ID(), r.PathValue("instance_id"))
	}))
}

// activeHosting mirrors WebHostingMustBeActive: a change on a web hosting that is not active is
// refused with 403.
func (s *Server) activeHosting(w http.ResponseWriter, scope Scope, id string) (Object, bool) {
	hosting, ok := s.lookup(KindWebHosting, scope, "", id)
	if !ok {
		writeNotFound(w)
		return nil, false
	}
	if hosting.String("status") != "active" {
		writeError(w, http.StatusForbidden, "This hosting account is not active. Wait for provisioning to complete "+
			"before performing actions on it.", "permission_denied")
		return nil, false
	}
	return hosting, true
}

func orderError(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, Object{"error": "Failed to create hosting order. Please try again later."})
}

// orderWebHosting mirrors create_hosting_order: the web hosting is created pending before the
// checkout, and its id answered with the order; a paid order activates it after the configured
// polls, an unpaid one (WithUnpaidHostingOrders) leaves it pending until PayHostingOrder.
func (s *Server) orderWebHosting(w http.ResponseWriter, r *http.Request, scope Scope) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	planID := errs.requireString(body, "plan_id", 0)
	if planID != "" && uuid.Validate(planID) != nil {
		errs.add("plan_id", "Must be a valid UUID.")
	}
	source := errs.requireString(body, "domain_source", 0)
	if source != "" && !slices.Contains(hostingDomainSources, source) {
		errs.add("domain_source", fmt.Sprintf("%q is not a valid choice.", source))
	}
	cycle := "monthly"
	if raw, present := body["billing_cycle"]; present {
		cycle, _ = raw.(string)
		if !slices.Contains(hostingBillingCycles, cycle) {
			errs.add("billing_cycle", fmt.Sprintf("%q is not a valid choice.", cycle))
		}
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	plan, ok := s.lookup(KindHostingPlan, Scope{}, "", planID)
	if !ok || plan["is_active"] == false {
		orderError(w)
		return
	}
	price := hostingPlanPrice(plan, cycle)
	domain := strings.ToLower(strings.TrimSpace(body.String("domain_name")))
	switch {
	case price == "", domain == "":
		orderError(w)
		return
	case source == "transfer" && body.String("epp_code") == "":
		orderError(w)
		return
	case source == "existing" && !slices.ContainsFunc(s.list(KindDomain, scope, ""), func(d Object) bool { return d.String("name") == domain }):
		orderError(w)
		return
	case source == "register" && strings.HasPrefix(domain, "taken-"):
		writeJSON(w, http.StatusConflict, Object{
			"error": "Domain not available for registration", "domain_name": domain, "available": false, "suggestions": []any{},
		})
		return
	}

	hosting := s.SeedWebHosting(scope, Object{
		"domain":                    domain,
		"username":                  "",
		"status":                    "pending",
		"plan":                      plan,
		"billing_cycle":             cycle,
		"price":                     price,
		"disk_limit_mb":             plan["disk_space_mb"],
		"bandwidth_limit_mb":        plan["bandwidth_mb"],
		"cancellation_requested_at": nil,
		"cancellation_reason_code":  "",
		"cancellation_reason":       "",
		"suspension_reason":         "",
	})
	id := hosting.ID()
	s.mu.Lock()
	s.hosting.init()
	s.hosting.unlinked[id] = true
	s.mu.Unlock()
	s.afterPolls(KindWebHosting, id, 1, func(Object) { delete(s.hosting.unlinked, id) })
	amount := priceFloat(price)
	answer := Object{
		"order_id":            len(s.All(KindWebHosting)) + 3000,
		"order_number":        fmt.Sprintf("%d", len(s.All(KindWebHosting))+7000),
		"invoice_id":          nil,
		"local_order_id":      uuid.NewString(),
		"total":               amount,
		"amount_due":          0.0,
		"credit_applied":      amount,
		"status":              "Active",
		"invoice_payment_url": nil,
		"hosting_id":          id,
		"hosting_status":      "pending",
		"domain_id":           nil,
		"domain_name":         domain,
	}
	if s.hosting.unpaidOrders {
		s.mu.Lock()
		s.hosting.init()
		s.hosting.awaitingPayment[id] = true
		s.mu.Unlock()
		answer["invoice_id"] = 910001
		answer["amount_due"] = amount * 1.2
		answer["credit_applied"] = 0.0
		answer["status"] = "Unpaid"
		answer["invoice_payment_url"] = "https://console.onetsolutions.net/billing/invoices/910001"
	} else {
		s.activateAfterPolls(id)
	}
	writeJSON(w, http.StatusCreated, answer)
}

func (s *Server) activateAfterPolls(id string) {
	s.afterPolls(KindWebHosting, id, s.taskPolls, func(o Object) {
		o["status"] = "active"
		o["username"] = "tfuser" + id[:6]
		o["next_billing_date"] = now()
	})
}

// PayHostingOrder pays the order of a web hosting WithUnpaidHostingOrders left pending: it is
// linked to its billing service and activated after the usual polls.
func (s *Server) PayHostingOrder(hostingID string) {
	s.mu.Lock()
	s.hosting.init()
	due := s.hosting.awaitingPayment[hostingID]
	delete(s.hosting.awaitingPayment, hostingID)
	s.mu.Unlock()
	if !due {
		s.t.Fatalf("fakeapi: web hosting %s has no unpaid order", hostingID)
	}
	s.activateAfterPolls(hostingID)
}

// hostingPlanPrice returns the price of a plan on a billing cycle, empty when the plan is not sold
// on it.
func hostingPlanPrice(plan Object, cycle string) string {
	for _, raw := range plan.List("pricing") {
		option, _ := raw.(map[string]any)
		if option["billing_cycle"] == cycle {
			price, _ := option["price"].(string)
			return price
		}
	}
	return ""
}

// cancelWebHosting mirrors cancel_web_hosting: the reason is validated, then the cancellation is
// requested at the end of the billing period; an ended hosting, a second request and a hosting
// not linked to its billing service yet are refused.
func (s *Server) cancelWebHosting(w http.ResponseWriter, r *http.Request, scope Scope) {
	id := r.PathValue("id")
	hosting, ok := s.lookup(KindWebHosting, scope, "", id)
	if !ok {
		writeNotFound(w)
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	code := ""
	if raw, present := body["reason_code"]; present {
		code, _ = raw.(string)
		if !slices.Contains(cancellationReasonCodes, code) {
			errs.add("reason_code", fmt.Sprintf("%q is not a valid choice.", code))
		}
	}
	reason := errs.requireString(body, "reason", 1000)
	if reason != "" && len(cancellationReasonWord.FindAllString(reason, -1)) < 7 {
		errs.add("reason", "Tell us why in at least 7 words, so we can act on it.")
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	s.mu.Lock()
	s.hosting.init()
	unlinked := s.hosting.unlinked[id]
	s.mu.Unlock()
	switch {
	case hosting.String("status") == "terminated" || hosting.String("status") == "cancelled":
		writeError(w, http.StatusBadRequest, "Operation failed. Please try again later.", "hosting_not_cancellable")
		return
	case hosting["cancellation_requested_at"] != nil:
		writeError(w, http.StatusBadRequest, "Cancellation already requested", "hosting_cancellation_already_requested")
		return
	case unlinked:
		writeError(w, http.StatusBadRequest, "Operation failed. Please try again later.", "hosting_not_linked_to_billing")
		return
	}
	updated, _ := s.update(KindWebHosting, scope, "", id, func(o Object) {
		o["cancellation_requested_at"] = now()
		o["cancellation_reason"] = reason
		o["cancellation_reason_code"] = code
	})
	writeJSON(w, http.StatusOK, updated)
}

// eligibleHostingPlans returns the active plans other than the hosting's own, as the API lists
// them: a bare array.
func (s *Server) eligibleHostingPlans(hosting Object) []Object {
	current := hosting.Map("plan").ID()
	out := []Object{}
	for _, plan := range s.list(KindHostingPlan, Scope{}, "") {
		if plan.ID() != current && plan["is_active"] != false {
			out = append(out, plan)
		}
	}
	return out
}

// changeHostingPlan mirrors hosting_change_plan: an upgrade order on an eligible plan, applied
// after one read unless WithUnpaidHostingPlanChanges leaves it unpaid.
func (s *Server) changeHostingPlan(w http.ResponseWriter, r *http.Request, scope Scope) {
	hosting, ok := s.activeHosting(w, scope, r.PathValue("id"))
	if !ok {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	planID := errs.requireString(body, "plan_id", 0)
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	id := hosting.ID()
	if planID == hosting.Map("plan").ID() {
		writeError(w, http.StatusBadRequest, "The web hosting is already on this plan.", "hosting_billing_change_refused")
		return
	}
	var target Object
	for _, plan := range s.eligibleHostingPlans(hosting) {
		if plan.ID() == planID {
			target = plan
		}
	}
	if target == nil {
		writeError(w, http.StatusBadRequest, "This plan is not an eligible upgrade for the web hosting.", "hosting_billing_change_refused")
		return
	}
	cycle := hosting.String("billing_cycle")
	if raw, _ := body["billing_cycle"].(string); raw != "" {
		if hostingCycleRank[raw] == 0 || hostingCycleRank[raw] < hostingCycleRank[cycle] {
			writeError(w, http.StatusBadRequest, "Only a longer billing cycle can be chosen.", "hosting_billing_change_refused")
			return
		}
		cycle = raw
	}
	price := hostingPlanPrice(target, cycle)
	if price == "" {
		writeError(w, http.StatusBadRequest, "The plan is not sold on this billing cycle.", "hosting_billing_change_refused")
		return
	}
	s.mu.Lock()
	s.hosting.init()
	due := s.hosting.planChangeDue[id]
	if s.hosting.unpaidPlanChanges {
		s.hosting.planChangeDue[id] = true
	}
	s.mu.Unlock()
	if due {
		writeError(w, http.StatusConflict, "A previous plan change of this service is awaiting payment. Pay its payment "+
			"notice before ordering another one.", "plan_change_awaiting_payment")
		return
	}
	invoiceURL := any(nil)
	if s.hosting.unpaidPlanChanges {
		invoiceURL = "https://console.onetsolutions.net/billing/invoices/920001"
	} else {
		s.afterPolls(KindWebHosting, id, 1, func(o Object) {
			o["plan"] = target
			o["billing_cycle"] = cycle
			o["price"] = price
			o["disk_limit_mb"] = target["disk_space_mb"]
		})
	}
	writeJSON(w, http.StatusAccepted, Object{
		"order_id":    3100,
		"invoice_id":  nil,
		"invoice_url": invoiceURL,
		"message":     "Plan change order created successfully. Please complete payment to finalize the change.",
	})
}

// hostingDomains returns the main domain of a hosting followed by its other domains: the seeded
// ones, its subdomains and its addon domains.
func (s *Server) hostingDomains(scope Scope, hosting Object) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosting.init()
	domains := append([]string{hosting.String("domain")}, s.hosting.extraDomains[hosting.ID()]...)
	for _, kind := range []Kind{KindHostingSubdomain, KindHostingAddonDomain} {
		for _, child := range s.store.list(kind, scope, hosting.ID()) {
			if !slices.Contains(domains, child.ID()) {
				domains = append(domains, child.ID())
			}
		}
	}
	return domains
}

// SeedHostingDomain adds a domain (a subdomain or an addon domain) to a web hosting.
func (s *Server) SeedHostingDomain(hostingID, domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosting.init()
	s.hosting.extraDomains[hostingID] = append(s.hosting.extraDomains[hostingID], domain)
}

// HostingPHPVersion returns the PHP version a domain of a web hosting runs.
func (s *Server) HostingPHPVersion(hostingID, domain string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosting.init()
	if version, ok := s.hosting.phpVersions[hostingID][domain]; ok {
		return version
	}
	return DefaultPHPVersion
}

// SetHostingPHPVersion changes the PHP version of a domain, as if changed outside Terraform.
func (s *Server) SetHostingPHPVersion(hostingID, domain, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosting.init()
	if s.hosting.phpVersions[hostingID] == nil {
		s.hosting.phpVersions[hostingID] = map[string]string{}
	}
	s.hosting.phpVersions[hostingID][domain] = version
}

// setHostingPHPVersion mirrors set_hosting_php_version: the domain defaults to the main one, and
// a version or a domain the account does not have is refused like a failed control panel call.
func (s *Server) setHostingPHPVersion(w http.ResponseWriter, r *http.Request, scope Scope) {
	hosting, ok := s.activeHosting(w, scope, r.PathValue("id"))
	if !ok {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	version := errs.requireString(body, "php_version", 0)
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	domain := body.String("domain")
	if domain == "" {
		domain = hosting.String("domain")
	}
	if (version != "inherit" && !slices.Contains(InstalledPHPVersions, version)) || !slices.Contains(s.hostingDomains(scope, hosting), domain) {
		writeError(w, http.StatusBadRequest, "Operation failed. Please try again later.", "hosting_operation_failed")
		return
	}
	s.SetHostingPHPVersion(hosting.ID(), domain, version)
	writeJSON(w, http.StatusOK, Object{"success": true})
}

// SeedWordPressSite adds a WordPress site to a web hosting, on its main domain unless overridden.
func (s *Server) SeedWordPressSite(scope Scope, hostingID string, overrides Object) Object {
	hosting, ok := s.Get(KindWebHosting, hostingID)
	if !ok {
		s.t.Fatalf("fakeapi: no web hosting %s", hostingID)
	}
	return s.insert(KindWordPressSite, scope, hostingID, merge(Object{
		"id":      s.nextWordPressID(),
		"domain":  hosting.String("domain"),
		"path":    "",
		"title":   "WordPress",
		"version": "6.5.0",
	}, overrides))
}

func (s *Server) nextWordPressID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosting.wordPressSeq++
	return fmt.Sprintf("26_%d", 40000+s.hosting.wordPressSeq)
}

// installWordPress mirrors install_hosting_wordpress: the input is validated, the domain must be
// one of the account's and run a recent enough PHP version, and the site is answered with an
// empty version, as the API does.
func (s *Server) installWordPress(w http.ResponseWriter, r *http.Request, scope Scope) {
	hosting, ok := s.activeHosting(w, scope, r.PathValue("id"))
	if !ok {
		return
	}
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	domain := errs.requireString(body, "domain", 255)
	errs.requireString(body, "admin_user", 60)
	errs.requireString(body, "admin_password", 0)
	if email := errs.requireString(body, "admin_email", 0); email != "" && !strings.Contains(email, "@") {
		errs.add("admin_email", "Enter a valid email address.")
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	path := body.String("path")
	title := body.String("title")
	if title == "" {
		title = "WordPress"
	}
	if !slices.Contains(s.hostingDomains(scope, hosting), domain) {
		writeError(w, http.StatusBadRequest, "Operation failed. Please try again later.", "wordpress_operation_failed")
		return
	}
	for _, site := range s.list(KindWordPressSite, scope, hosting.ID()) {
		if site.String("domain") == domain && site.String("path") == path {
			writeError(w, http.StatusBadRequest, "Operation failed. Please try again later.", "wordpress_operation_failed")
			return
		}
	}
	if php := s.HostingPHPVersion(hosting.ID(), domain); php != "inherit" && php < WordPressMinPHPVersion {
		writeError(w, http.StatusBadRequest, "WordPress requires PHP 8.1 or higher, but this domain runs "+php+
			". Change the domain's PHP version before installing.", "wordpress_requirement_not_met")
		return
	}
	site := s.insert(KindWordPressSite, scope, hosting.ID(), Object{
		"id": s.nextWordPressID(), "domain": domain, "path": path, "title": title, "version": WordPressLatestVersion,
	})
	writeJSON(w, http.StatusCreated, Object{"id": site.ID(), "domain": domain, "path": path, "title": title, "version": ""})
}
