package client

import (
	"context"
	"net/http"
)

// HostingDatabase is the Database schema: a MySQL database of a web hosting. Name carries the
// account prefix ("<username>_").
type HostingDatabase struct {
	Name   string   `json:"name"`
	SizeMB float64  `json:"size_mb"`
	Users  []string `json:"users"`
}

// HostingDatabaseUser is the DatabaseUser schema; Username carries the account prefix.
type HostingDatabaseUser struct {
	Username string `json:"username"`
}

// HostingEmailAccount is the EmailAccount schema. A quota of 0 means unlimited.
type HostingEmailAccount struct {
	Email       string `json:"email"`
	User        string `json:"user"`
	Domain      string `json:"domain"`
	QuotaMB     int64  `json:"quota_mb"`
	QuotaUsedMB int64  `json:"quota_used_mb"`
	IsMain      bool   `json:"is_main"`
}

// HostingFTPAccount is the FTPAccount schema. Directory is the absolute path the account is
// confined to; a quota of 0 means unlimited.
type HostingFTPAccount struct {
	User        string `json:"user"`
	Directory   string `json:"directory"`
	QuotaMB     int64  `json:"quota_mb"`
	QuotaUsedMB int64  `json:"quota_used_mb"`
}

// CreateHostingEmailAccountRequest is the CreateEmailInput schema.
type CreateHostingEmailAccountRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	QuotaMB  int64  `json:"quota_mb"`
}

// CreateHostingFTPAccountRequest is the CreateFTPAccountInput schema.
type CreateHostingFTPAccountRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Directory string `json:"directory"`
	QuotaMB   int64  `json:"quota_mb"`
}

const hostingMailDBPath = webHostingsPath + "/{id}"

var (
	routeListHostingDatabases       = newRoute(http.MethodGet, hostingMailDBPath+"/databases")
	routeCreateHostingDatabase      = newRoute(http.MethodPost, hostingMailDBPath+"/databases/create")
	routeDeleteHostingDatabase      = newRoute(http.MethodDelete, hostingMailDBPath+"/databases/{name}/delete")
	routeListHostingDatabaseUsers   = newRoute(http.MethodGet, hostingMailDBPath+"/database-users")
	routeCreateHostingDatabaseUser  = newRoute(http.MethodPost, hostingMailDBPath+"/database-users/create")
	routeDeleteHostingDatabaseUser  = newRoute(http.MethodDelete, hostingMailDBPath+"/database-users/{username}/delete")
	routeSetHostingDatabasePrivs    = newRoute(http.MethodPost, hostingMailDBPath+"/database-privileges")
	routeListHostingEmails          = newRoute(http.MethodGet, hostingMailDBPath+"/emails")
	routeCreateHostingEmail         = newRoute(http.MethodPost, hostingMailDBPath+"/emails/create")
	routeDeleteHostingEmail         = newRoute(http.MethodDelete, hostingMailDBPath+"/emails/{email}/delete")
	routeChangeHostingEmailPassword = newRoute(http.MethodPost, hostingMailDBPath+"/emails/{email}/password")
	routeSetHostingEmailQuota       = newRoute(http.MethodPost, hostingMailDBPath+"/emails/{email}/quota")
	routeListHostingFTPAccounts     = newRoute(http.MethodGet, hostingMailDBPath+"/ftp-accounts")
	routeCreateHostingFTPAccount    = newRoute(http.MethodPost, hostingMailDBPath+"/ftp-accounts/create")
	routeDeleteHostingFTPAccount    = newRoute(http.MethodDelete, hostingMailDBPath+"/ftp-accounts/{username}/delete")
	routeChangeHostingFTPPassword   = newRoute(http.MethodPost, hostingMailDBPath+"/ftp-accounts/{username}/password")
)

// ListHostingDatabases lists the databases of a web hosting.
func (c *Client) ListHostingDatabases(ctx context.Context, scope Scope, hostingID string) ([]HostingDatabase, error) {
	return listAll[HostingDatabase](ctx, c, routeListHostingDatabases, scope.Params("id", hostingID), nil)
}

// CreateHostingDatabase creates a database; the API prefixes name with the account's username
// unless it already starts with it, and returns the full name.
func (c *Client) CreateHostingDatabase(ctx context.Context, scope Scope, hostingID, name string) (*HostingDatabase, error) {
	var out HostingDatabase
	if err := c.do(ctx, routeCreateHostingDatabase, scope.Params("id", hostingID), map[string]string{"name": name}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteHostingDatabase deletes a database and its data.
func (c *Client) DeleteHostingDatabase(ctx context.Context, scope Scope, hostingID, name string) error {
	return c.do(ctx, routeDeleteHostingDatabase, scope.Params("id", hostingID, "name", name), nil, nil)
}

// ListHostingDatabaseUsers lists the database users of a web hosting.
func (c *Client) ListHostingDatabaseUsers(ctx context.Context, scope Scope, hostingID string) ([]HostingDatabaseUser, error) {
	return listAll[HostingDatabaseUser](ctx, c, routeListHostingDatabaseUsers, scope.Params("id", hostingID), nil)
}

// CreateHostingDatabaseUser creates a database user, prefixed like a database.
func (c *Client) CreateHostingDatabaseUser(ctx context.Context, scope Scope, hostingID, username, password string) (*HostingDatabaseUser, error) {
	var out HostingDatabaseUser
	body := map[string]string{"username": username, "password": password}
	if err := c.do(ctx, routeCreateHostingDatabaseUser, scope.Params("id", hostingID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteHostingDatabaseUser deletes a database user and its privileges.
func (c *Client) DeleteHostingDatabaseUser(ctx context.Context, scope Scope, hostingID, username string) error {
	return c.do(ctx, routeDeleteHostingDatabaseUser, scope.Params("id", hostingID, "username", username), nil, nil)
}

// SetHostingDatabasePrivileges sets the privileges of a user on a database, such as
// "ALL PRIVILEGES" or "SELECT,INSERT".
func (c *Client) SetHostingDatabasePrivileges(ctx context.Context, scope Scope, hostingID, username, database, privileges string) error {
	body := map[string]string{"username": username, "database": database, "privileges": privileges}
	return c.do(ctx, routeSetHostingDatabasePrivs, scope.Params("id", hostingID), body, nil)
}

// ListHostingEmailAccounts lists the mailboxes of a web hosting, the main one included.
func (c *Client) ListHostingEmailAccounts(ctx context.Context, scope Scope, hostingID string) ([]HostingEmailAccount, error) {
	return listAll[HostingEmailAccount](ctx, c, routeListHostingEmails, scope.Params("id", hostingID), nil)
}

// CreateHostingEmailAccount creates a mailbox.
func (c *Client) CreateHostingEmailAccount(ctx context.Context, scope Scope, hostingID string, in CreateHostingEmailAccountRequest) (*HostingEmailAccount, error) {
	var out HostingEmailAccount
	if err := c.do(ctx, routeCreateHostingEmail, scope.Params("id", hostingID), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteHostingEmailAccount deletes a mailbox and its messages.
func (c *Client) DeleteHostingEmailAccount(ctx context.Context, scope Scope, hostingID, email string) error {
	return c.do(ctx, routeDeleteHostingEmail, scope.Params("id", hostingID, "email", email), nil, nil)
}

// ChangeHostingEmailPassword sets the password of a mailbox.
func (c *Client) ChangeHostingEmailPassword(ctx context.Context, scope Scope, hostingID, email, password string) error {
	body := map[string]string{"password": password}
	return c.do(ctx, routeChangeHostingEmailPassword, scope.Params("id", hostingID, "email", email), body, nil)
}

// SetHostingEmailQuota sets the quota of a mailbox in MB, 0 for unlimited.
func (c *Client) SetHostingEmailQuota(ctx context.Context, scope Scope, hostingID, email string, quotaMB int64) error {
	body := map[string]int64{"quota_mb": quotaMB}
	return c.do(ctx, routeSetHostingEmailQuota, scope.Params("id", hostingID, "email", email), body, nil)
}

// ListHostingFTPAccounts lists the FTP accounts of a web hosting, the main one included.
func (c *Client) ListHostingFTPAccounts(ctx context.Context, scope Scope, hostingID string) ([]HostingFTPAccount, error) {
	return listAll[HostingFTPAccount](ctx, c, routeListHostingFTPAccounts, scope.Params("id", hostingID), nil)
}

// CreateHostingFTPAccount creates an FTP account.
func (c *Client) CreateHostingFTPAccount(ctx context.Context, scope Scope, hostingID string, in CreateHostingFTPAccountRequest) (*HostingFTPAccount, error) {
	var out HostingFTPAccount
	if err := c.do(ctx, routeCreateHostingFTPAccount, scope.Params("id", hostingID), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteHostingFTPAccount deletes an FTP account; the files of its directory stay.
func (c *Client) DeleteHostingFTPAccount(ctx context.Context, scope Scope, hostingID, username string) error {
	return c.do(ctx, routeDeleteHostingFTPAccount, scope.Params("id", hostingID, "username", username), nil, nil)
}

// ChangeHostingFTPPassword sets the password of an FTP account.
func (c *Client) ChangeHostingFTPPassword(ctx context.Context, scope Scope, hostingID, username, password string) error {
	body := map[string]string{"password": password}
	return c.do(ctx, routeChangeHostingFTPPassword, scope.Params("id", hostingID, "username", username), body, nil)
}
