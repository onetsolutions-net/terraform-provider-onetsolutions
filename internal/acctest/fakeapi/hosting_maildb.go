package fakeapi

import (
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// The databases, database users, mailboxes and FTP accounts of a web hosting. Their parent is
// the web hosting; their store ID is HostingObjectID(hosting, key), the key being the full
// database or user name, the address or the FTP username the API identifies them by.
const (
	KindHostingDatabase     Kind = "hosting_database"
	KindHostingDatabaseUser Kind = "hosting_database_user"
	KindHostingEmail        Kind = "hosting_email"
	KindHostingFTPAccount   Kind = "hosting_ftp_account"
)

const (
	hostingOperationFailed = "Operation failed. Please try again later."
	hostingInactiveDetail  = "This hosting account is not active. Wait for provisioning to complete before performing actions on it."
	mainMailboxDetail      = "The main mailbox of a hosting plan belongs to its account and cannot be deleted. Change its password instead."
	weakPasswordDetail     = "Password must be at least 8 characters and include uppercase, lowercase, a digit and a special character."
	defaultEmailQuotaMB    = 250
	defaultFTPDirectory    = "/public_html"
	allPrivileges          = "ALL PRIVILEGES"
	hostingFailedCode      = "hosting_operation_failed"
)

var (
	emailPattern   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	specialPattern = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

// HostingObjectID is the store ID of a hosting sub-object, for Get, Exists, Delete and Mutate.
func HostingObjectID(hostingID, key string) string { return hostingID + "/" + key }

// HostingPrefixed is the name the API gives a database or database user: the account's
// username and an underscore, then the name, unless it already starts with them.
func HostingPrefixed(username, name string) string {
	prefix := username + "_"
	if strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}

// HostingMainMailbox is the address of the main mailbox of a web hosting.
func HostingMainMailbox(hosting Object) string {
	return hosting.String("username") + "@" + hosting.String("domain")
}

// SeedHostingDatabase adds a database to a web hosting, name prefixed like the API does.
func (s *Server) SeedHostingDatabase(scope Scope, hosting Object, name string) Object {
	full := HostingPrefixed(hosting.String("username"), name)
	return s.insert(KindHostingDatabase, scope, hosting.ID(), Object{
		"id": HostingObjectID(hosting.ID(), full), "name": full, "size_mb": 1.5, "users": []any{}, "privileges": Object{},
	})
}

// SeedHostingDatabaseUser adds a database user to a web hosting.
func (s *Server) SeedHostingDatabaseUser(scope Scope, hosting Object, username, password string) Object {
	full := HostingPrefixed(hosting.String("username"), username)
	return s.insert(KindHostingDatabaseUser, scope, hosting.ID(), Object{
		"id": HostingObjectID(hosting.ID(), full), "username": full, "password": password,
	})
}

// SeedHostingEmail adds a mailbox to a web hosting.
func (s *Server) SeedHostingEmail(scope Scope, hosting Object, email string, quotaMB int) Object {
	user, domain, _ := strings.Cut(email, "@")
	return s.insert(KindHostingEmail, scope, hosting.ID(), Object{
		"id": HostingObjectID(hosting.ID(), email), "email": email, "user": user, "domain": domain,
		"quota_mb": quotaMB, "quota_used_mb": 3, "is_main": false, "password": "Seeded-1",
	})
}

// SeedHostingFTPAccount adds an FTP account to a web hosting, directory relative to its home.
func (s *Server) SeedHostingFTPAccount(scope Scope, hosting Object, username, directory string, quotaMB int) Object {
	return s.insert(KindHostingFTPAccount, scope, hosting.ID(), Object{
		"id": HostingObjectID(hosting.ID(), username), "user": username,
		"directory": ftpHome(hosting, directory), "quota_mb": quotaMB, "quota_used_mb": 0, "password": "Seeded-1",
	})
}

func ftpHome(hosting Object, directory string) string {
	return "/home/" + hosting.String("username") + "/" + strings.TrimPrefix(directory, "/")
}

func registerHostingMailDB(s *Server) {
	base := webHostingsTemplate + "/{id}"

	s.handle(http.MethodGet, base+"/databases", s.withHosting(false, func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		items := s.list(KindHostingDatabase, scope, hosting.ID())
		out := make([]Object, 0, len(items))
		for _, db := range items {
			out = append(out, Object{"name": db["name"], "size_mb": db["size_mb"], "users": db["users"]})
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, base+"/databases/create", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		name := errs.requireString(body, "name", 16)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		full := HostingPrefixed(hosting.String("username"), name)
		if _, exists := s.lookup(KindHostingDatabase, scope, hosting.ID(), HostingObjectID(hosting.ID(), full)); exists {
			writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
			return
		}
		s.insert(KindHostingDatabase, scope, hosting.ID(), Object{
			"id": HostingObjectID(hosting.ID(), full), "name": full, "size_mb": 0, "users": []any{}, "privileges": Object{},
		})
		writeJSON(w, http.StatusCreated, Object{"name": full, "size_mb": 0, "users": []any{}})
	}))
	s.handle(http.MethodDelete, base+"/databases/{name}/delete", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		full := HostingPrefixed(hosting.String("username"), r.PathValue("name"))
		s.removeHostingObject(w, KindHostingDatabase, scope, hosting, full)
	}))

	s.handle(http.MethodGet, base+"/database-users", s.withHosting(false, func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		items := s.list(KindHostingDatabaseUser, scope, hosting.ID())
		out := make([]Object, 0, len(items))
		for _, user := range items {
			out = append(out, Object{"username": user["username"]})
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, base+"/database-users/create", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		username := errs.requireString(body, "username", 16)
		password := errs.requirePassword(body)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		full := HostingPrefixed(hosting.String("username"), username)
		if _, exists := s.lookup(KindHostingDatabaseUser, scope, hosting.ID(), HostingObjectID(hosting.ID(), full)); exists {
			writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
			return
		}
		s.insert(KindHostingDatabaseUser, scope, hosting.ID(), Object{
			"id": HostingObjectID(hosting.ID(), full), "username": full, "password": password,
		})
		writeJSON(w, http.StatusCreated, Object{"username": full})
	}))
	s.handle(http.MethodDelete, base+"/database-users/{username}/delete", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		full := HostingPrefixed(hosting.String("username"), r.PathValue("username"))
		if !s.remove(KindHostingDatabaseUser, scope, hosting.ID(), HostingObjectID(hosting.ID(), full)) {
			writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
			return
		}
		for _, db := range s.list(KindHostingDatabase, scope, hosting.ID()) {
			s.update(KindHostingDatabase, scope, hosting.ID(), db.ID(), func(o Object) { revoke(o, full) })
		}
		writeJSON(w, http.StatusNoContent, nil)
	}))
	s.handle(http.MethodPost, base+"/database-privileges", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		username := errs.requireString(body, "username", 0)
		database := errs.requireString(body, "database", 0)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		privileges := allPrivileges
		if raw, present := body["privileges"].(string); present {
			privileges = raw
		}
		prefix := hosting.String("username")
		user, database := HostingPrefixed(prefix, username), HostingPrefixed(prefix, database)
		_, userExists := s.lookup(KindHostingDatabaseUser, scope, hosting.ID(), HostingObjectID(hosting.ID(), user))
		valid := userExists && validPrivileges(privileges)
		_, updated := s.update(KindHostingDatabase, scope, hosting.ID(), HostingObjectID(hosting.ID(), database), func(o Object) {
			if valid {
				grant(o, user, privileges)
			}
		})
		if !valid || !updated {
			writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
			return
		}
		writeJSON(w, http.StatusOK, Object{"success": true})
	}))

	s.handle(http.MethodGet, base+"/emails", s.withHosting(false, func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		main := HostingMainMailbox(hosting)
		out := []Object{}
		var others []Object
		mainListed := false
		for _, mailbox := range s.list(KindHostingEmail, scope, hosting.ID()) {
			view := Object{
				"email": mailbox["email"], "user": mailbox["user"], "domain": mailbox["domain"],
				"quota_mb": mailbox["quota_mb"], "quota_used_mb": mailbox["quota_used_mb"], "is_main": mailbox["email"] == main,
			}
			if view["is_main"] == true {
				mainListed = true
				out = append(out, view)
				continue
			}
			others = append(others, view)
		}
		if !mainListed {
			out = append(out, Object{
				"email": main, "user": hosting["username"], "domain": hosting["domain"],
				"quota_mb": 0, "quota_used_mb": 0, "is_main": true,
			})
		}
		sort.Slice(others, func(i, j int) bool { return others[i].String("email") < others[j].String("email") })
		writeJSON(w, http.StatusOK, append(out, others...))
	}))
	s.handle(http.MethodPost, base+"/emails/create", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		email := errs.requireString(body, "email", 0)
		if email != "" && !emailPattern.MatchString(email) {
			errs.add("email", "Enter a valid email address.")
		}
		password := errs.requirePassword(body)
		quota := errs.quota(body, defaultEmailQuotaMB, false)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		email = strings.ToLower(email)
		user, domain, _ := strings.Cut(email, "@")
		_, exists := s.lookup(KindHostingEmail, scope, hosting.ID(), HostingObjectID(hosting.ID(), email))
		if exists || domain != hosting.String("domain") || email == HostingMainMailbox(hosting) {
			writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
			return
		}
		s.insert(KindHostingEmail, scope, hosting.ID(), Object{
			"id": HostingObjectID(hosting.ID(), email), "email": email, "user": user, "domain": domain,
			"quota_mb": quota, "quota_used_mb": 0, "is_main": false, "password": password,
		})
		writeJSON(w, http.StatusCreated, Object{"email": email, "user": user, "domain": domain, "quota_mb": quota})
	}))
	s.handle(http.MethodDelete, base+"/emails/{email}/delete", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		email := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
		if email == strings.ToLower(HostingMainMailbox(hosting)) {
			writeFieldErrors(w, fieldErrors{"email": {mainMailboxDetail}})
			return
		}
		s.removeHostingObject(w, KindHostingEmail, scope, hosting, email)
	}))
	s.handle(http.MethodPost, base+"/emails/{email}/password", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		password := errs.requirePassword(body)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		email := strings.ToLower(r.PathValue("email"))
		if email == HostingMainMailbox(hosting) {
			s.ensureMainMailbox(scope, hosting)
		}
		s.updateHostingObject(w, KindHostingEmail, scope, hosting, email, func(o Object) { o["password"] = password })
	}))
	s.handle(http.MethodPost, base+"/emails/{email}/quota", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		quota := errs.quota(body, 0, true)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		email := strings.ToLower(r.PathValue("email"))
		if email == HostingMainMailbox(hosting) {
			s.ensureMainMailbox(scope, hosting)
		}
		s.updateHostingObject(w, KindHostingEmail, scope, hosting, email, func(o Object) { o["quota_mb"] = quota })
	}))

	s.handle(http.MethodGet, base+"/ftp-accounts", s.withHosting(false, func(w http.ResponseWriter, _ *http.Request, scope Scope, hosting Object) {
		out := []Object{{
			"user": hosting["username"], "directory": "/home/" + hosting.String("username"), "quota_mb": 0, "quota_used_mb": 0,
		}}
		for _, account := range s.list(KindHostingFTPAccount, scope, hosting.ID()) {
			out = append(out, Object{
				"user": account["user"], "directory": account["directory"],
				"quota_mb": account["quota_mb"], "quota_used_mb": account["quota_used_mb"],
			})
		}
		writeJSON(w, http.StatusOK, out)
	}))
	s.handle(http.MethodPost, base+"/ftp-accounts/create", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		username := errs.requireString(body, "username", 32)
		password := errs.requirePassword(body)
		quota := errs.quota(body, 0, false)
		directory := defaultFTPDirectory
		if raw, present := body["directory"].(string); present {
			directory = raw
		}
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		_, exists := s.lookup(KindHostingFTPAccount, scope, hosting.ID(), HostingObjectID(hosting.ID(), username))
		if exists || username == hosting.String("username") {
			writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
			return
		}
		s.insert(KindHostingFTPAccount, scope, hosting.ID(), Object{
			"id": HostingObjectID(hosting.ID(), username), "user": username, "directory": ftpHome(hosting, directory),
			"quota_mb": quota, "quota_used_mb": 0, "password": password,
		})
		writeJSON(w, http.StatusCreated, Object{"user": username, "directory": directory, "quota_mb": quota, "quota_used_mb": 0})
	}))
	s.handle(http.MethodDelete, base+"/ftp-accounts/{username}/delete", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		s.removeHostingObject(w, KindHostingFTPAccount, scope, hosting, r.PathValue("username"))
	}))
	s.handle(http.MethodPost, base+"/ftp-accounts/{username}/password", s.withHosting(true, func(w http.ResponseWriter, r *http.Request, scope Scope, hosting Object) {
		body, ok := decodeBody(w, r)
		if !ok {
			return
		}
		errs := fieldErrors{}
		password := errs.requirePassword(body)
		if len(errs) > 0 {
			writeFieldErrors(w, errs)
			return
		}
		s.updateHostingObject(w, KindHostingFTPAccount, scope, hosting, r.PathValue("username"), func(o Object) { o["password"] = password })
	}))
}

// withHosting resolves the web hosting of the path, answering 404 when it is not in the project
// and, for a change, 403 when it is not active, like the API.
func (s *Server) withHosting(change bool, handler func(http.ResponseWriter, *http.Request, Scope, Object)) http.HandlerFunc {
	return s.withScope(func(w http.ResponseWriter, r *http.Request, scope Scope) {
		hosting, ok := s.lookup(KindWebHosting, scope, "", r.PathValue("id"))
		if !ok {
			writeNotFound(w)
			return
		}
		if change && (hosting.String("status") != "active" || hosting["is_active"] != true) {
			writeJSON(w, http.StatusForbidden, Object{"detail": hostingInactiveDetail, "code": "permission_denied"})
			return
		}
		handler(w, r, scope, hosting)
	})
}

// removeHostingObject deletes a sub-object; a missing one is the hosting account's own failure,
// which the API reports as a failed operation rather than a not-found.
func (s *Server) removeHostingObject(w http.ResponseWriter, kind Kind, scope Scope, hosting Object, key string) {
	if !s.remove(kind, scope, hosting.ID(), HostingObjectID(hosting.ID(), key)) {
		writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) updateHostingObject(w http.ResponseWriter, kind Kind, scope Scope, hosting Object, key string, change func(Object)) {
	if _, ok := s.update(kind, scope, hosting.ID(), HostingObjectID(hosting.ID(), key), change); !ok {
		writeError(w, http.StatusBadRequest, hostingOperationFailed, hostingFailedCode)
		return
	}
	writeJSON(w, http.StatusOK, Object{"success": true})
}

// ensureMainMailbox stores the main mailbox the list serves as a placeholder, so a change to it
// can be recorded.
func (s *Server) ensureMainMailbox(scope Scope, hosting Object) {
	main := HostingMainMailbox(hosting)
	if _, ok := s.lookup(KindHostingEmail, scope, hosting.ID(), HostingObjectID(hosting.ID(), main)); ok {
		return
	}
	s.insert(KindHostingEmail, scope, hosting.ID(), Object{
		"id": HostingObjectID(hosting.ID(), main), "email": main, "user": hosting["username"], "domain": hosting["domain"],
		"quota_mb": 0, "quota_used_mb": 0, "is_main": true,
	})
}

func grant(db Object, user, privileges string) {
	users := db.List("users")
	listed := false
	for _, u := range users {
		if u == user {
			listed = true
		}
	}
	if !listed {
		users = append(users, user)
	}
	db["users"] = users
	grants := db.Map("privileges")
	if grants == nil {
		grants = Object{}
	}
	grants[user] = privileges
	db["privileges"] = grants
}

func revoke(db Object, user string) {
	users := []any{}
	for _, u := range db.List("users") {
		if u != user {
			users = append(users, u)
		}
	}
	db["users"] = users
	if grants := db.Map("privileges"); grants != nil {
		delete(grants, user)
	}
}

var mysqlPrivileges = map[string]bool{
	"ALTER": true, "ALTER ROUTINE": true, "CREATE": true, "CREATE ROUTINE": true, "CREATE TEMPORARY TABLES": true,
	"CREATE VIEW": true, "DELETE": true, "DROP": true, "EVENT": true, "EXECUTE": true, "INDEX": true, "INSERT": true,
	"LOCK TABLES": true, "REFERENCES": true, "SELECT": true, "SHOW VIEW": true, "TRIGGER": true, "UPDATE": true,
}

func validPrivileges(privileges string) bool {
	if privileges == allPrivileges {
		return true
	}
	for _, p := range strings.Split(privileges, ",") {
		if !mysqlPrivileges[strings.TrimSpace(p)] {
			return false
		}
	}
	return true
}

// requirePassword checks body["password"] like the API's strong password rule.
func (f fieldErrors) requirePassword(body Object) string {
	raw, present := body["password"]
	value, ok := raw.(string)
	switch {
	case !present || raw == nil:
		f.add("password", "This field is required.")
	case !ok:
		f.add("password", "Not a valid string.")
	case len(value) < 8 || !strings.ContainsAny(value, "abcdefghijklmnopqrstuvwxyz") ||
		!strings.ContainsAny(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || !strings.ContainsAny(value, "0123456789") ||
		!specialPattern.MatchString(value):
		f.add("password", weakPasswordDetail)
	}
	return value
}

// quota reads body["quota_mb"], a non-negative integer, defaulting to fallback unless required.
func (f fieldErrors) quota(body Object, fallback int, required bool) int {
	raw, present := body["quota_mb"]
	if !present || raw == nil {
		if required {
			f.add("quota_mb", "This field is required.")
		}
		return fallback
	}
	value, ok := raw.(float64)
	if !ok || value != math.Trunc(value) {
		f.add("quota_mb", "A valid integer is required.")
		return fallback
	}
	if value < 0 {
		f.add("quota_mb", "Ensure this value is greater than or equal to 0.")
		return fallback
	}
	return int(value)
}
