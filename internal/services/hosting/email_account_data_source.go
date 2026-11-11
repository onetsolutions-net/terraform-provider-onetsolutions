package hosting

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

type emailAccountItem struct {
	Email       types.String `tfsdk:"email"`
	User        types.String `tfsdk:"user"`
	Domain      types.String `tfsdk:"domain"`
	QuotaMB     types.Int64  `tfsdk:"quota_mb"`
	QuotaUsedMB types.Int64  `tfsdk:"quota_used_mb"`
	IsMain      types.Bool   `tfsdk:"is_main"`
}

// NewEmailAccountsDataSource returns the onetsolutions_hosting_email_accounts data source.
func NewEmailAccountsDataSource() datasource.DataSource {
	return &mailDBListDataSource[emailAccountItem]{
		typeName:    "_hosting_email_accounts",
		description: "Lists the mailboxes of a web hosting, its main mailbox first.",
		attribute:   "email_accounts",
		what:        "mailboxes",
		item: map[string]schema.Attribute{
			"email":         schema.StringAttribute{Computed: true, MarkdownDescription: "The address of the mailbox."},
			"user":          schema.StringAttribute{Computed: true, MarkdownDescription: "The login of the mailbox."},
			"domain":        schema.StringAttribute{Computed: true, MarkdownDescription: "The domain of the address."},
			"quota_mb":      schema.Int64Attribute{Computed: true, MarkdownDescription: "The disk quota in MB; 0 means unlimited."},
			"quota_used_mb": schema.Int64Attribute{Computed: true, MarkdownDescription: "The disk space used, in MB."},
			"is_main":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this is the web hosting's main mailbox."},
		},
		attrTypes: map[string]attr.Type{
			"email": types.StringType, "user": types.StringType, "domain": types.StringType,
			"quota_mb": types.Int64Type, "quota_used_mb": types.Int64Type, "is_main": types.BoolType,
		},
		list: func(ctx context.Context, c *client.Client, scope client.Scope, hostingID string) ([]emailAccountItem, error) {
			accounts, err := c.ListHostingEmailAccounts(ctx, scope, hostingID)
			if err != nil {
				return nil, err
			}
			items := make([]emailAccountItem, 0, len(accounts))
			for _, a := range accounts {
				items = append(items, emailAccountItem{
					Email: types.StringValue(a.Email), User: types.StringValue(a.User), Domain: types.StringValue(a.Domain),
					QuotaMB: types.Int64Value(a.QuotaMB), QuotaUsedMB: types.Int64Value(a.QuotaUsedMB), IsMain: types.BoolValue(a.IsMain),
				})
			}
			return items, nil
		},
	}
}
