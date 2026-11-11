package fakeapi

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Child kinds of a web hosting (parent: the web hosting). Their store IDs are the cron job's line
// key, the subdomain's full domain and the addon domain itself; redirects get a UUID, the API
// giving them no key.
const (
	KindHostingCronJob     Kind = "hosting_cron_job"
	KindHostingSubdomain   Kind = "hosting_subdomain"
	KindHostingAddonDomain Kind = "hosting_addon_domain"
	KindHostingRedirect    Kind = "hosting_redirect"
)

const hostingWebTemplate = "/v1/organizations/{organization_id}/projects/{project_id}/hosting/{id}"

// WebChildChangeFailed is the detail of the API's answer to a change the hosting refuses (a
// duplicate, an unknown name).
const WebChildChangeFailed = "Operation failed. Please try again later."

var (
	cronFieldPattern      = regexp.MustCompile(`^[0-9A-Za-z*/,-]+$`)
	addonSubdomainPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	addonLabelSeparators  = regexp.MustCompile(`[^a-z0-9]+`)
	cronSchedule          = []string{"minute", "hour", "day", "month", "weekday"}
	redirectListedTypes   = map[string]string{"301": "permanent", "302": "temp"}
)

// registerHostingWeb serves the cron jobs, subdomains, addon domains and redirects of a web
// hosting as the API does: lists as bare arrays, changes refused while the hosting is not active,
// a cron job created with an empty line key, subdomains listed with the default document root
// whatever their own, addon document roots listed as absolute paths, redirect types listed by
// name, and no way to delete a redirect.
func registerHostingWeb(s *Server) {
	s.handle(http.MethodGet, hostingWebTemplate+"/cron-jobs", s.withWebChildRead(func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		jobs := s.list(KindHostingCronJob, scope, hosting.ID())
		out := make([]Object, 0, len(jobs))
		for _, job := range jobs {
			out = append(out, cronJobView(job))
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, hostingWebTemplate+"/cron-jobs/create", s.withWebChildWrite(s.createHostingCronJob))
	s.handle(http.MethodPost, hostingWebTemplate+"/cron-jobs/delete", s.withWebChildWrite(func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		lineKey := errs.requireString(body, "linekey", 0)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		s.removeWebChild(w, KindHostingCronJob, scope, hosting.ID(), lineKey)
	}))

	s.handle(http.MethodGet, hostingWebTemplate+"/subdomains", s.withWebChildRead(func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		subdomains := s.list(KindHostingSubdomain, scope, hosting.ID())
		out := make([]Object, 0, len(subdomains))
		for _, subdomain := range subdomains {
			full := subdomain.ID()
			label := strings.Split(full, ".")[0]
			out = append(out, Object{
				"subdomain":     label,
				"domain":        hosting.String("domain"),
				"full_domain":   full,
				"document_root": "/home/" + hosting.String("username") + "/public_html/" + label,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, hostingWebTemplate+"/subdomains/create", s.withWebChildWrite(s.createHostingSubdomain))
	s.handle(http.MethodDelete, hostingWebTemplate+"/subdomains/{subdomain}/delete", s.withWebChildWrite(func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		full := r.PathValue("subdomain")
		if !strings.Contains(full, ".") {
			full += "." + hosting.String("domain")
		}
		s.removeWebChild(w, KindHostingSubdomain, scope, hosting.ID(), full)
	}))

	s.handle(http.MethodGet, hostingWebTemplate+"/addon-domains", s.withWebChildRead(func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		domains := s.list(KindHostingAddonDomain, scope, hosting.ID())
		out := make([]Object, 0, len(domains))
		for _, domain := range domains {
			out = append(out, webChildView(domain, "domain", "subdomain", "document_root"))
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, hostingWebTemplate+"/addon-domains/create", s.withWebChildWrite(s.createHostingAddonDomain))
	s.handle(http.MethodDelete, hostingWebTemplate+"/addon-domains/{domain}/delete", s.withWebChildWrite(func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		s.removeWebChild(w, KindHostingAddonDomain, scope, hosting.ID(), r.PathValue("domain"))
	}))

	s.handle(http.MethodGet, hostingWebTemplate+"/redirects", s.withWebChildRead(func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		redirects := s.list(KindHostingRedirect, scope, hosting.ID())
		out := make([]Object, 0, len(redirects))
		for _, redirect := range redirects {
			listed := webChildView(redirect, "domain", "source", "destination", "type")
			if name, ok := redirectListedTypes[redirect.String("type")]; ok {
				listed["type"] = name
			}
			out = append(out, listed)
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, hostingWebTemplate+"/redirects/create", s.withWebChildWrite(s.createHostingRedirect))
}

func webChildView(o Object, fields ...string) Object {
	out := Object{}
	for _, field := range fields {
		out[field] = o.String(field)
	}
	return out
}

func cronJobView(job Object) Object {
	out := webChildView(job, append([]string{"command"}, cronSchedule...)...)
	out["linekey"] = job.ID()
	return out
}

func (s *Server) withWebChild(write bool, handler func(http.ResponseWriter, *http.Request, Scope, Object)) http.HandlerFunc {
	return s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		hosting, ok := s.lookup(KindWebHosting, scope, "", r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "Web hosting not found.", "not_found")
			return
		}
		switch {
		case write && (hosting["is_active"] != true || hosting.String("status") != "active"):
			writeError(w, http.StatusForbidden,
				"This hosting account is not active. Wait for provisioning to complete before performing actions on it.",
				"permission_denied")
			return
		case hosting.String("status") == "terminated":
			writeError(w, http.StatusBadRequest, WebChildChangeFailed, "cpanel_operation_failed")
			return
		}
		handler(w, r, scope, hosting)
	})
}

func (s *Server) withWebChildRead(handler func(http.ResponseWriter, *http.Request, Scope, Object)) http.HandlerFunc {
	return s.withWebChild(false, handler)
}

func (s *Server) withWebChildWrite(handler func(http.ResponseWriter, *http.Request, Scope, Object)) http.HandlerFunc {
	return s.withWebChild(true, handler)
}

func writeWebChildFailure(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, WebChildChangeFailed, "cpanel_operation_failed")
}

// removeWebChild deletes a child the way the API does: an unknown one is a failed operation,
// not a 404.
func (s *Server) removeWebChild(w http.ResponseWriter, kind Kind, scope Scope, hostingID, id string) {
	if !s.remove(kind, scope, hostingID, id) {
		writeWebChildFailure(w)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func newLineKey() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

func (s *Server) createHostingCronJob(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	job := Object{"id": newLineKey(), "command": errs.requireString(body, "command", 0)}
	for _, field := range cronSchedule {
		value := "*"
		if _, present := body[field]; present {
			value = errs.requireString(body, field, 0)
		}
		if value != "" && !cronFieldPattern.MatchString(value) {
			errs.add(field, "Enter one cron value for this field, such as */5, 0 or 1-5, without spaces.")
		}
		job[field] = value
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	s.insert(KindHostingCronJob, scope, hosting.ID(), job)
	created := cronJobView(job)
	created["linekey"] = ""
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) createHostingSubdomain(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	label := errs.requireString(body, "subdomain", 63)
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	label = strings.ToLower(label)
	full := label + "." + hosting.String("domain")
	if _, exists := s.lookup(KindHostingSubdomain, scope, hosting.ID(), full); exists {
		writeWebChildFailure(w)
		return
	}
	root := trimmedField(body, "document_root")
	s.insert(KindHostingSubdomain, scope, hosting.ID(), Object{"id": full, "document_root": root})
	if root == "" {
		root = "/public_html/" + label
	}
	writeJSON(w, http.StatusCreated, Object{
		"subdomain": label, "domain": hosting.String("domain"), "full_domain": full, "document_root": root,
	})
}

func addonSubdomain(domain string) string {
	label := strings.Trim(addonLabelSeparators.ReplaceAllString(strings.ToLower(domain), "-"), "-")
	if len(label) > 63 {
		label = strings.TrimRight(label[:63], "-")
	}
	return label
}

func (s *Server) createHostingAddonDomain(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	domain := strings.ToLower(errs.requireString(body, "domain", 255))
	subdomain := trimmedField(body, "subdomain")
	if subdomain != "" && !addonSubdomainPattern.MatchString(subdomain) {
		errs.add("subdomain", "Use letters, digits and hyphens only, without dots (63 characters at most).")
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	if _, exists := s.lookup(KindHostingAddonDomain, scope, hosting.ID(), domain); exists {
		writeWebChildFailure(w)
		return
	}
	if subdomain == "" {
		subdomain = addonSubdomain(domain)
	}
	root := trimmedField(body, "document_root")
	home := "/home/" + hosting.String("username")
	listed := home + "/public_html/" + domain
	if root != "" {
		listed = home + "/" + strings.TrimPrefix(root, "/")
	}
	s.insert(KindHostingAddonDomain, scope, hosting.ID(), Object{
		"id": domain, "domain": domain, "subdomain": subdomain, "document_root": listed,
	})
	if root == "" {
		root = "/public_html/" + domain
	}
	writeJSON(w, http.StatusCreated, Object{"domain": domain, "subdomain": subdomain, "document_root": root})
}

func (s *Server) createHostingRedirect(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	errs := fieldErrors{}
	redirect := Object{
		"domain":      errs.requireString(body, "domain", 0),
		"source":      errs.requireString(body, "source", 0),
		"destination": errs.requireString(body, "destination", 0),
		"type":        "301",
	}
	if destination := redirect.String("destination"); destination != "" {
		parsed, err := url.Parse(destination)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			errs.add("destination", "Enter a valid URL.")
		}
	}
	if _, present := body["type"]; present {
		redirect["type"] = errs.requireString(body, "type", 0)
	}
	if len(errs) > 0 {
		writeFieldErrors(w, errs)
		return
	}
	s.insert(KindHostingRedirect, scope, hosting.ID(), redirect)
	writeJSON(w, http.StatusCreated, webChildView(redirect, "domain", "source", "destination", "type"))
}

func (s *Server) webHostingOf(hostingID string) Object {
	hosting, ok := s.Get(KindWebHosting, hostingID)
	if !ok {
		s.t.Fatalf("fakeapi: no web hosting %s", hostingID)
	}
	return hosting
}

// SeedHostingCronJob adds a cron job to a web hosting, running every minute unless overridden.
func (s *Server) SeedHostingCronJob(scope Scope, hostingID string, overrides Object) Object {
	s.webHostingOf(hostingID)
	job := merge(Object{"id": newLineKey(), "command": "php -q /home/seeded/cron.php",
		"minute": "*", "hour": "*", "day": "*", "month": "*", "weekday": "*"}, overrides)
	return s.insert(KindHostingCronJob, scope, hostingID, job)
}

// SeedHostingSubdomain adds a subdomain of the hosting's main domain.
func (s *Server) SeedHostingSubdomain(scope Scope, hostingID, label string) Object {
	hosting := s.webHostingOf(hostingID)
	return s.insert(KindHostingSubdomain, scope, hostingID, Object{"id": label + "." + hosting.String("domain"), "document_root": ""})
}

// SeedHostingAddonDomain adds an addon domain to a web hosting.
func (s *Server) SeedHostingAddonDomain(scope Scope, hostingID, domain string) Object {
	hosting := s.webHostingOf(hostingID)
	return s.insert(KindHostingAddonDomain, scope, hostingID, Object{
		"id": domain, "domain": domain, "subdomain": addonSubdomain(domain),
		"document_root": "/home/" + hosting.String("username") + "/public_html/" + domain,
	})
}

// SeedHostingRedirect adds a redirect to a web hosting.
func (s *Server) SeedHostingRedirect(scope Scope, hostingID string, overrides Object) Object {
	hosting := s.webHostingOf(hostingID)
	return s.insert(KindHostingRedirect, scope, hostingID, merge(Object{
		"domain": hosting.String("domain"), "source": "/old", "destination": "https://example.com/", "type": "301",
	}, overrides))
}
