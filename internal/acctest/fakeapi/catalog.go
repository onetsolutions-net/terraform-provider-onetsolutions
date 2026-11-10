package fakeapi

import (
	"net/http"
)

const (
	sizesTemplate         = "/v1/compute/sizes"
	projectImagesTemplate = "/v1/organizations/{organization_id}/projects/{project_id}/compute/images"
	webHostingsTemplate   = "/v1/organizations/{organization_id}/projects/{project_id}/hosting"
)

// registerCatalog serves the global catalog (anonymous, as in the API), the
// images a project can install, and the web hostings of a project.
func registerCatalog(s *Server) {
	s.handle(http.MethodGet, sizesTemplate, func(w http.ResponseWriter, r *http.Request) {
		writePage(w, r, s.list(KindSize, Scope{}, ""))
	}, anonymous)
	s.handle(http.MethodGet, sizesTemplate+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.writeObject(w, KindSize, Scope{}, "", r.PathValue("id"))
	}, anonymous)
	s.handle(http.MethodGet, "/v1/compute/images/public", func(w http.ResponseWriter, r *http.Request) {
		writePage(w, r, s.list(KindPublicImage, Scope{}, ""))
	}, anonymous)
	s.handle(http.MethodGet, "/v1/compute/control-panels", func(w http.ResponseWriter, r *http.Request) {
		writePage(w, r, s.list(KindControlPanel, Scope{}, ""))
	}, anonymous)
	s.handle(http.MethodGet, "/v1/hosting/plans", func(w http.ResponseWriter, r *http.Request) {
		writePage(w, r, s.list(KindHostingPlan, Scope{}, ""))
	}, anonymous)

	s.handle(http.MethodGet, projectImagesTemplate, s.withScope(s.listProjectImages))
	s.handle(http.MethodGet, projectImagesTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		id := r.PathValue("id")
		if image, ok := s.lookup(KindPrivateImage, scope, "", id); ok {
			writeJSON(w, http.StatusOK, image)
			return
		}
		s.writeObject(w, KindPublicImage, Scope{}, "", id)
	}))

	s.handle(http.MethodGet, webHostingsTemplate, s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		hostings := newestFirst(s.list(KindWebHosting, scope, ""))
		summaries := make([]Object, 0, len(hostings))
		for _, hosting := range hostings {
			summaries = append(summaries, Object{
				"id":            hosting["id"],
				"project_id":    scope.ProjectID,
				"domain":        hosting["domain"],
				"username":      hosting["username"],
				"status":        hosting["status"],
				"plan_name":     hosting.Map("plan").String("name"),
				"billing_cycle": hosting["billing_cycle"],
				"price":         hosting["price"],
				"is_active":     hosting["is_active"],
				"created":       hosting["created"],
			})
		}
		writePage(w, r, summaries)
	}))
	s.handle(http.MethodGet, webHostingsTemplate+"/{id}", s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		s.writeObject(w, KindWebHosting, scope, "", r.PathValue("id"))
	}))
}

// listProjectImages lists the images of a project as the API does: public and private images, or
// one visibility when ?visibility= says so, newest first.
func (s *Server) listProjectImages(w http.ResponseWriter, r *http.Request, scope Scope) {
	visibility := r.URL.Query().Get("visibility")
	var images []Object
	if visibility != "public" {
		images = append(images, s.list(KindPrivateImage, scope, "")...)
	}
	if visibility != "private" {
		images = append(images, s.list(KindPublicImage, Scope{}, "")...)
	}
	writePage(w, r, images)
}
