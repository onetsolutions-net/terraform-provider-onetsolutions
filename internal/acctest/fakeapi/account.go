package fakeapi

import "net/http"

// registerAccount serves organizations and projects. Both list endpoints return a bare JSON
// array, as the API does.
func registerAccount(s *Server) {
	s.handle(http.MethodGet, "/v1/organizations", func(w http.ResponseWriter, _ *http.Request) {
		orgs := s.list(KindOrganization, Scope{}, "")
		for _, org := range orgs {
			org["project_count"] = len(s.list(KindProject, Scope{OrganizationID: org.ID()}, ""))
		}
		writeJSON(w, http.StatusOK, orgs)
	})
	s.handle(http.MethodGet, "/v1/organizations/{id}", func(w http.ResponseWriter, r *http.Request) {
		org, ok := s.lookup(KindOrganization, Scope{}, "", r.PathValue("id"))
		if !ok {
			writeNotFound(w)
			return
		}
		org["project_count"] = len(s.list(KindProject, Scope{OrganizationID: org.ID()}, ""))
		writeJSON(w, http.StatusOK, org)
	})
	s.handle(http.MethodGet, "/v1/organizations/{organization_id}/projects", func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("organization_id")
		if _, ok := s.lookupQuiet(KindOrganization, Scope{}, "", orgID); !ok {
			writeNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, s.list(KindProject, Scope{OrganizationID: orgID}, ""))
	})
	s.handle(http.MethodGet, "/v1/organizations/{organization_id}/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		project, ok := s.lookup(KindProject, Scope{OrganizationID: r.PathValue("organization_id")}, "", r.PathValue("id"))
		if !ok {
			writeNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, project)
	})
}
