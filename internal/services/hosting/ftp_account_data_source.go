package hosting

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

type ftpAccountItem struct {
	Username      types.String `tfsdk:"username"`
	HomeDirectory types.String `tfsdk:"home_directory"`
	QuotaMB       types.Int64  `tfsdk:"quota_mb"`
	QuotaUsedMB   types.Int64  `tfsdk:"quota_used_mb"`
}

// NewFTPAccountsDataSource returns the onetsolutions_hosting_ftp_accounts data source.
func NewFTPAccountsDataSource() datasource.DataSource {
	return &mailDBListDataSource[ftpAccountItem]{
		typeName:    "_hosting_ftp_accounts",
		description: "Lists the FTP accounts of a web hosting, its main account included.",
		attribute:   "ftp_accounts",
		what:        "FTP accounts",
		item: map[string]schema.Attribute{
			"username":       schema.StringAttribute{Computed: true, MarkdownDescription: "The username of the account."},
			"home_directory": schema.StringAttribute{Computed: true, MarkdownDescription: "The absolute path of the account's directory."},
			"quota_mb":       schema.Int64Attribute{Computed: true, MarkdownDescription: "The disk quota in MB; 0 means unlimited."},
			"quota_used_mb":  schema.Int64Attribute{Computed: true, MarkdownDescription: "The disk space used, in MB."},
		},
		attrTypes: map[string]attr.Type{
			"username": types.StringType, "home_directory": types.StringType,
			"quota_mb": types.Int64Type, "quota_used_mb": types.Int64Type,
		},
		list: func(ctx context.Context, c *client.Client, scope client.Scope, hostingID string) ([]ftpAccountItem, error) {
			accounts, err := c.ListHostingFTPAccounts(ctx, scope, hostingID)
			if err != nil {
				return nil, err
			}
			items := make([]ftpAccountItem, 0, len(accounts))
			for _, a := range accounts {
				items = append(items, ftpAccountItem{
					Username: types.StringValue(a.User), HomeDirectory: types.StringValue(a.Directory),
					QuotaMB: types.Int64Value(a.QuotaMB), QuotaUsedMB: types.Int64Value(a.QuotaUsedMB),
				})
			}
			return items, nil
		},
	}
}
