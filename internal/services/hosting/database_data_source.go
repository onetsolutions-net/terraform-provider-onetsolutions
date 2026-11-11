package hosting

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

// mailDBListDataSource lists one kind of object of a web hosting into a list attribute; I is the
// item model, with tfsdk tags matching attributes.
type mailDBListDataSource[I any] struct {
	common.DataSourceBase
	typeName    string
	description string
	attribute   string
	what        string
	item        map[string]schema.Attribute
	attrTypes   map[string]attr.Type
	list        func(ctx context.Context, c *client.Client, scope client.Scope, hostingID string) ([]I, error)
}

var _ datasource.DataSourceWithConfigure = (*mailDBListDataSource[databaseItem])(nil)

func (d *mailDBListDataSource[I]) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + d.typeName
}

func (d *mailDBListDataSource[I]) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: d.description,
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			mailDBHostingIDAttr: mailDBDataSourceHostingAttribute(),
			d.attribute: schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The " + d.what + ".",
				NestedObject:        schema.NestedAttributeObject{Attributes: d.item},
			},
		}),
	}
}

func (d *mailDBListDataSource[I]) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var organizationID, projectID, hostingID types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(common.AttrOrganizationID), &organizationID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(common.AttrProjectID), &projectID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(mailDBHostingIDAttr), &hostingID)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := d.ResolveScope(ctx, organizationID, projectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	items, err := d.list(ctx, c, scope, hostingID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the "+d.what, err)
		return
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: d.attrTypes}, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrOrganizationID), scope.OrganizationID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(common.AttrProjectID), scope.ProjectID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(mailDBHostingIDAttr), hostingID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(d.attribute), list)...)
}

type databaseItem struct {
	Name   types.String  `tfsdk:"name"`
	SizeMB types.Float64 `tfsdk:"size_mb"`
	Users  types.List    `tfsdk:"users"`
}

// NewDatabasesDataSource returns the onetsolutions_hosting_databases data source.
func NewDatabasesDataSource() datasource.DataSource {
	return &mailDBListDataSource[databaseItem]{
		typeName:    "_hosting_databases",
		description: "Lists the MySQL databases of a web hosting.",
		attribute:   "databases",
		what:        "databases",
		item: map[string]schema.Attribute{
			"name":    schema.StringAttribute{Computed: true, MarkdownDescription: "The full name of the database, prefixed with the web hosting's username."},
			"size_mb": schema.Float64Attribute{Computed: true, MarkdownDescription: "The disk space the database uses, in MB."},
			"users": schema.ListAttribute{
				Computed: true, ElementType: types.StringType,
				MarkdownDescription: "The full usernames of the users with privileges on the database.",
			},
		},
		attrTypes: map[string]attr.Type{
			"name": types.StringType, "size_mb": types.Float64Type, "users": types.ListType{ElemType: types.StringType},
		},
		list: func(ctx context.Context, c *client.Client, scope client.Scope, hostingID string) ([]databaseItem, error) {
			databases, err := c.ListHostingDatabases(ctx, scope, hostingID)
			if err != nil {
				return nil, err
			}
			items := make([]databaseItem, 0, len(databases))
			for _, database := range databases {
				users := make([]attr.Value, 0, len(database.Users))
				for _, user := range database.Users {
					users = append(users, types.StringValue(user))
				}
				items = append(items, databaseItem{
					Name: types.StringValue(database.Name), SizeMB: types.Float64Value(database.SizeMB),
					Users: types.ListValueMust(types.StringType, users),
				})
			}
			return items, nil
		},
	}
}
