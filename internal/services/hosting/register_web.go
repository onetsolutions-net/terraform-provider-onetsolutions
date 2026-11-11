package hosting

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

// webResources returns the resources managing the content of a web hosting: cron jobs,
// subdomains, addon domains and redirects.
func webResources() []func() resource.Resource {
	return []func() resource.Resource{
		NewCronJobResource,
		NewSubdomainResource,
		NewAddonDomainResource,
		NewRedirectResource,
	}
}

// webDataSources returns the data sources listing the content of a web hosting.
func webDataSources() []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCronJobsDataSource,
		NewSubdomainsDataSource,
		NewAddonDomainsDataSource,
		NewRedirectsDataSource,
		NewWordPressSitesDataSource,
	}
}

const attrHostingID = "hosting_id"

// webDomainPattern is a lowercase fully qualified domain name without a trailing dot.
var webDomainPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func webDomainValidators() []validator.String {
	return []validator.String{
		stringvalidator.LengthAtMost(253),
		stringvalidator.RegexMatches(webDomainPattern, "must be a lowercase domain name without a trailing dot, such as example.com"),
	}
}

func hostingIDAttribute(what string) schema.StringAttribute {
	return schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "The UUID of the web hosting the " + what + " belongs to. Changing it recreates the " + what + ".",
		Validators:          []validator.String{common.UUID()},
		PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

func hostingIDDataSourceAttribute() dsschema.StringAttribute {
	return dsschema.StringAttribute{
		Required:            true,
		MarkdownDescription: "The UUID of the web hosting to read from.",
		Validators:          []validator.String{common.UUID()},
	}
}

// webChildImportID is a parsed "<organization_id>/<project_id>/<hosting_id>/<key>" or
// "<hosting_id>/<key>". The key may itself hold slashes (a redirect's path).
type webChildImportID struct {
	OrganizationID, ProjectID, HostingID, Key string
}

func parseWebChildImportID(raw, keyName string) (webChildImportID, error) {
	parts := strings.Split(raw, "/")
	forms := fmt.Sprintf("expected <organization_id>/<project_id>/<hosting_id>/<%s>, or <hosting_id>/<%s> when the provider sets organization_id and project_id", keyName, keyName)
	switch {
	case len(parts) >= 4 && common.IsUUID(parts[0]) && common.IsUUID(parts[1]) && common.IsUUID(parts[2]):
		id := webChildImportID{OrganizationID: parts[0], ProjectID: parts[1], HostingID: parts[2], Key: strings.Join(parts[3:], "/")}
		if strings.TrimSpace(id.Key) != "" {
			return id, nil
		}
	case len(parts) >= 2 && common.IsUUID(parts[0]) && !common.IsUUID(parts[1]):
		id := webChildImportID{HostingID: parts[0], Key: strings.Join(parts[1:], "/")}
		if strings.TrimSpace(id.Key) != "" {
			return id, nil
		}
	}
	return webChildImportID{}, fmt.Errorf("unexpected import ID %q; %s", raw, forms)
}

// importWebChild writes the scope, hosting_id and the key (as id) of a web hosting child being
// imported. Read finds the object by that key and fills the rest.
func importWebChild(ctx context.Context, b *common.ResourceBase, req resource.ImportStateRequest, resp *resource.ImportStateResponse, keyName string) {
	id, err := parseWebChildImportID(req.ID, keyName)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	if id.OrganizationID == "" {
		scope, diags := b.ResolveScope(ctx, types.StringNull(), types.StringNull())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		id.OrganizationID, id.ProjectID = scope.OrganizationID, scope.ProjectID
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrID), id.Key)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrOrganizationID), id.OrganizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrProjectID), id.ProjectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(attrHostingID), id.HostingID)...)
}

// webChildList lists the children of a web hosting for Read and Delete. gone is true, with no
// error, when the hosting itself is gone.
func webChildList[T any](ctx context.Context, c *client.Client, scope client.Scope, hostingID string,
	list func(context.Context, client.Scope, string) ([]T, error),
) (items []T, gone bool, err error) {
	gone, err = webHostingGone(ctx, c, scope, hostingID)
	if err != nil || gone {
		return nil, gone, err
	}
	items, err = list(ctx, scope, hostingID)
	if client.IsNotFound(err) {
		return nil, true, nil
	}
	return items, false, err
}

// webChildDeleteFailed handles the error of a delete: nil when the hosting turned out to be gone
// meanwhile, the error otherwise.
func webChildDeleteFailed(ctx context.Context, c *client.Client, scope client.Scope, hostingID string, err error, diags *diag.Diagnostics, summary string) {
	if client.IsNotFound(err) {
		return
	}
	if client.IsForbidden(err) {
		if gone, readErr := webHostingGone(ctx, c, scope, hostingID); readErr == nil && gone {
			return
		}
	}
	common.AddClientError(diags, summary, err)
}

// sameDirectory reports whether a directory the hosting reports is the one configured: the
// hosting reports absolute paths, while a configuration may give one relative to the account's
// home directory.
func sameDirectory(configured, reported string) bool {
	configured = strings.TrimSuffix(configured, "/")
	reported = strings.TrimSuffix(reported, "/")
	if configured == reported {
		return true
	}
	relative := strings.TrimPrefix(configured, "/")
	return relative != "" && strings.HasSuffix(reported, "/"+relative)
}

func webScope(organizationID, projectID types.String) client.Scope {
	return client.Scope{OrganizationID: organizationID.ValueString(), ProjectID: projectID.ValueString()}
}

type webListFunc[T any] func(context.Context, client.Scope, string) ([]T, error)

// webListDataSource lists one kind of child of a web hosting; every item attribute is a string.
type webListDataSource[T any] struct {
	common.DataSourceBase
	typeName    string
	description string
	listName    string
	listWhat    string
	attributes  map[string]string // attribute name → description
	list        func(*client.Client) webListFunc[T]
	values      func(*T) map[string]string
}

func (d *webListDataSource[T]) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + d.typeName
}

func (d *webListDataSource[T]) itemType() types.ObjectType {
	attrTypes := map[string]attr.Type{}
	for name := range d.attributes {
		attrTypes[name] = types.StringType
	}
	return types.ObjectType{AttrTypes: attrTypes}
}

func (d *webListDataSource[T]) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	item := map[string]dsschema.Attribute{}
	for name, description := range d.attributes {
		item[name] = dsschema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = dsschema.Schema{
		MarkdownDescription: d.description,
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]dsschema.Attribute{
			attrHostingID: hostingIDDataSourceAttribute(),
			d.listName: dsschema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: d.listWhat,
				NestedObject:        dsschema.NestedAttributeObject{Attributes: item},
			},
		}),
	}
}

func (d *webListDataSource[T]) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var organizationID, projectID, hostingID types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(common.AttrOrganizationID), &organizationID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(common.AttrProjectID), &projectID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(attrHostingID), &hostingID)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := d.ResolveScope(ctx, organizationID, projectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	items, err := d.list(c)(ctx, scope, hostingID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the "+strings.ReplaceAll(d.listName, "_", " ")+" of the web hosting", err)
		return
	}
	itemType := d.itemType()
	elements := make([]attr.Value, 0, len(items))
	for i := range items {
		values := map[string]attr.Value{}
		for name, value := range d.values(&items[i]) {
			values[name] = types.StringValue(value)
		}
		element, elementDiags := types.ObjectValue(itemType.AttrTypes, values)
		resp.Diagnostics.Append(elementDiags...)
		elements = append(elements, element)
	}
	list, diags := types.ListValue(itemType, elements)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrOrganizationID), scope.OrganizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrProjectID), scope.ProjectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(attrHostingID), hostingID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(d.listName), list)...)
}
