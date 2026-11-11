package hosting

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

// mailDBResources returns the databases, database users and privileges, mailboxes and FTP
// accounts of a web hosting.
func mailDBResources() []func() resource.Resource {
	return []func() resource.Resource{
		NewDatabaseResource,
		NewDatabaseUserResource,
		NewDatabasePrivilegesResource,
		NewEmailAccountResource,
		NewFTPAccountResource,
	}
}

// mailDBDataSources returns the lists of those objects.
func mailDBDataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDatabasesDataSource,
		NewDatabaseUsersDataSource,
		NewEmailAccountsDataSource,
		NewFTPAccountsDataSource,
	}
}

const (
	mailDBHostingIDAttr  = "hosting_id"
	mailDBProtectionAttr = "deletion_protection"
	mailDBAllPrivileges  = "ALL PRIVILEGES"
)

func mailDBHostingAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "The UUID of the web hosting. Changing it recreates the object in the other web hosting.",
		Validators:          []validator.String{common.UUID()},
		PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

func mailDBDataSourceHostingAttribute() dsschema.StringAttribute {
	return dsschema.StringAttribute{
		Required:            true,
		MarkdownDescription: "The UUID of the web hosting.",
		Validators:          []validator.String{common.UUID()},
	}
}

func mailDBIDAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: description,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func mailDBScope(organizationID, projectID types.String) client.Scope {
	return client.Scope{OrganizationID: organizationID.ValueString(), ProjectID: projectID.ValueString()}
}

// mailDBHosting reads a web hosting. gone is true when it no longer exists for its owner or has
// ended (terminated or cancelled): what it held went with it.
func mailDBHosting(ctx context.Context, c *client.Client, scope client.Scope, hostingID string) (hosting *client.WebHosting, gone bool, err error) {
	hosting, err = c.GetWebHosting(ctx, scope, hostingID)
	if client.IsNotFound(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return hosting, hosting.Status == "terminated" || hosting.Status == "cancelled", nil
}

// mailDBGoneWithHosting reports whether a failed change means the web hosting is gone: a
// not-found, or a refusal because it has ended.
func mailDBGoneWithHosting(ctx context.Context, c *client.Client, scope client.Scope, hostingID string, err error) bool {
	if client.IsNotFound(err) {
		return true
	}
	if !client.IsForbidden(err) {
		return false
	}
	_, gone, readErr := mailDBHosting(ctx, c, scope, hostingID)
	return readErr == nil && gone
}

// mailDBMayHaveSucceeded reports whether a failed create may still have created the object: a
// server error or a lost connection, as opposed to a refusal.
func mailDBMayHaveSucceeded(err error) bool {
	if client.IsCanceled(err) {
		return false
	}
	apiErr, ok := client.AsAPIError(err)
	return !ok || apiErr.StatusCode >= 500
}

// mailDBPrefixed is the full name of a database or database user: the hosting account's username
// and an underscore, then the name, unless the name already starts with them.
func mailDBPrefixed(username, name string) string {
	prefix := username + "_"
	if strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}

// mailDBShort is a full name without the account prefix.
func mailDBShort(username, full string) string {
	return strings.TrimPrefix(full, username+"_")
}

// mailDBPlanName lets a configured name be written with or without the account prefix: a new
// spelling of the same full name is an in-place change, any other change a replacement. A name
// unknown until apply, such as the full name of a database being replaced, is a replacement too:
// an update would keep the previous full name.
func mailDBPlanName(ctx context.Context, data *common.ProviderData, scope client.Scope, hostingID string, planned, stateName, stateFull types.String, attribute string, resp *resource.ModifyPlanResponse) {
	if planned.Equal(stateName) {
		return
	}
	if planned.IsUnknown() {
		if !stateName.IsNull() {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root(attribute))
		}
		return
	}
	if !planned.IsNull() && !stateFull.IsNull() && data.Ready() {
		var diags diag.Diagnostics
		c := data.RequireClient(&diags)
		if c != nil {
			hosting, err := c.GetWebHosting(ctx, scope, hostingID)
			if err == nil && mailDBPrefixed(hosting.Username, planned.ValueString()) == stateFull.ValueString() {
				return
			}
		}
	}
	resp.RequiresReplace = append(resp.RequiresReplace, path.Root(attribute))
}

// mailDBReadChild reads the state of a child resource, the client and the web hosting it lives
// in. ok is false when Read should stop: on an error, or once the resource left the state because
// the hosting is gone.
func mailDBReadChild(ctx context.Context, c *client.Client, scope client.Scope, hostingID string, resp *resource.ReadResponse) (*client.WebHosting, bool) {
	hosting, gone, err := mailDBHosting(ctx, c, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return nil, false
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return nil, false
	}
	return hosting, true
}

// mailDBImport imports a child of a web hosting from
// "<organization_id>/<project_id>/<hosting_id>/<key...>" or "<hosting_id>/<key...>", keyParts
// being how many parts the key has. It writes the key, joined by "/", as the id.
func mailDBImport(ctx context.Context, b *common.ResourceBase, req resource.ImportStateRequest, resp *resource.ImportStateResponse, keyParts int, keyForm string) {
	parts := strings.Split(req.ID, "/")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			parts = nil
			break
		}
	}
	var organizationID, projectID string
	switch len(parts) {
	case 3 + keyParts:
		organizationID, projectID, parts = parts[0], parts[1], parts[2:]
	case 1 + keyParts:
		scope, diags := b.ResolveScope(ctx, types.StringNull(), types.StringNull())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		organizationID, projectID = scope.OrganizationID, scope.ProjectID
	default:
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf(
			"unexpected import ID %q; expected <organization_id>/<project_id>/<hosting_id>/%s, or <hosting_id>/%s "+
				"when the provider sets organization_id and project_id", req.ID, keyForm, keyForm))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrOrganizationID), organizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrProjectID), projectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(mailDBHostingIDAttr), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrID), strings.Join(parts[1:], "/"))...)
}

func mailDBDeletionProtectionError(diags *diag.Diagnostics, what, key string) {
	diags.AddError("The "+what+" is protected from deletion",
		fmt.Sprintf("The %s %s has deletion_protection = true. Set it to false and apply before destroying or replacing it.", what, key))
}

// mailDBAbsent reports whether key is no longer listed, after a delete the API refused: deleting
// what is already gone is a failed operation, not a not-found.
func mailDBAbsent[T any](ctx context.Context, list func(context.Context) ([]T, error), match func(T) bool) bool {
	items, err := list(ctx)
	if err != nil {
		return false
	}
	for _, item := range items {
		if match(item) {
			return false
		}
	}
	return true
}

// mailDBStrongPassword checks the API's password rule at plan time.
type mailDBStrongPassword struct{}

func (mailDBStrongPassword) Description(context.Context) string {
	return "value must have at least 8 characters with an uppercase letter, a lowercase letter, a digit and a special character"
}

func (v mailDBStrongPassword) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (mailDBStrongPassword) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	var lower, upper, digit, special bool
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			special = true
		}
	}
	if len(value) < 8 || !lower || !upper || !digit || !special {
		resp.Diagnostics.AddAttributeError(req.Path, "Weak password",
			"The password must have at least 8 characters and include an uppercase letter, a lowercase letter, a digit and a special character.")
	}
}
