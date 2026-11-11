package hosting

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

type databaseUserItem struct {
	Username types.String `tfsdk:"username"`
}

// NewDatabaseUsersDataSource returns the onetsolutions_hosting_database_users data source.
func NewDatabaseUsersDataSource() datasource.DataSource {
	return &mailDBListDataSource[databaseUserItem]{
		typeName:    "_hosting_database_users",
		description: "Lists the MySQL users of a web hosting.",
		attribute:   "database_users",
		what:        "database users",
		item: map[string]schema.Attribute{
			"username": schema.StringAttribute{Computed: true, MarkdownDescription: "The full username, prefixed with the web hosting's username."},
		},
		attrTypes: map[string]attr.Type{"username": types.StringType},
		list: func(ctx context.Context, c *client.Client, scope client.Scope, hostingID string) ([]databaseUserItem, error) {
			users, err := c.ListHostingDatabaseUsers(ctx, scope, hostingID)
			if err != nil {
				return nil, err
			}
			items := make([]databaseUserItem, 0, len(users))
			for _, user := range users {
				items = append(items, databaseUserItem{Username: types.StringValue(user.Username)})
			}
			return items, nil
		},
	}
}
