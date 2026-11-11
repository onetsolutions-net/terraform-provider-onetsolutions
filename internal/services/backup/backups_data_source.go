package backup

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var _ datasource.DataSourceWithConfigure = (*instanceBackupsDataSource)(nil)

var backupStatuses = []string{
	client.BackupPending, client.BackupRunning, client.BackupCompleted, client.BackupFailed,
	client.BackupDeleting, client.BackupDeleted,
}

// NewInstanceBackupsDataSource returns the onetsolutions_instance_backups data source.
func NewInstanceBackupsDataSource() datasource.DataSource {
	return &instanceBackupsDataSource{}
}

type instanceBackupsDataSource struct {
	common.DataSourceBase
}

type instanceBackupsModel struct {
	OrganizationID types.String      `tfsdk:"organization_id"`
	ProjectID      types.String      `tfsdk:"project_id"`
	InstanceID     types.String      `tfsdk:"instance_id"`
	Status         types.String      `tfsdk:"status"`
	Backups        []backupItemModel `tfsdk:"backups"`
}

type backupItemModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Status        types.String `tfsdk:"status"`
	SizeGB        types.String `tfsdk:"size_gb"`
	RetentionDays types.Int64  `tfsdk:"retention_days"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
	BackupType    types.String `tfsdk:"backup_type"`
	Created       types.String `tfsdk:"created"`
}

func (d *instanceBackupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_backups"
}

func (d *instanceBackupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the backups of an instance, newest first, on-demand and scheduled ones alike.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the instance.",
				Validators:          []validator.String{common.UUID()},
			},
			"status": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Only list the backups with this status, such as `completed` for those that can be " +
					"restored. When unset, every backup is listed.",
				Validators: []validator.String{stringvalidator.OneOf(backupStatuses...)},
			},
			"backups": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The backups, newest first.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the backup."},
					"name":           schema.StringAttribute{Computed: true, MarkdownDescription: "The name of the backup."},
					"status":         schema.StringAttribute{Computed: true, MarkdownDescription: "The status of the backup: `pending`, `running`, `completed`, `failed`, `deleting` or `deleted`."},
					"size_gb":        schema.StringAttribute{Computed: true, MarkdownDescription: "The size of the backup in GB, a decimal; null until it is complete."},
					"retention_days": schema.Int64Attribute{Computed: true, MarkdownDescription: "How many days the backup is kept."},
					"expires_at":     schema.StringAttribute{Computed: true, MarkdownDescription: "When the backup expires (RFC 3339)."},
					"backup_type":    schema.StringAttribute{Computed: true, MarkdownDescription: "`manual` or `scheduled`."},
					"created":        schema.StringAttribute{Computed: true, MarkdownDescription: "When the backup was requested (RFC 3339)."},
				}},
			},
		}),
	}
}

func (d *instanceBackupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config instanceBackupsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := d.ResolveScope(ctx, config.OrganizationID, config.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	backups, err := c.ListBackups(ctx, scope, config.InstanceID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the backups", err)
		return
	}
	config.OrganizationID = types.StringValue(scope.OrganizationID)
	config.ProjectID = types.StringValue(scope.ProjectID)
	config.Backups = []backupItemModel{}
	for i := range backups {
		backup := &backups[i]
		if !config.Status.IsNull() && backup.Status != config.Status.ValueString() {
			continue
		}
		config.Backups = append(config.Backups, backupItemModel{
			ID:            types.StringValue(backup.ID),
			Name:          types.StringValue(backup.Name),
			Status:        types.StringValue(backup.Status),
			SizeGB:        types.StringPointerValue(backup.SizeGB),
			RetentionDays: types.Int64Value(backup.RetentionDays),
			ExpiresAt:     types.StringPointerValue(backup.ExpiresAt),
			BackupType:    types.StringValue(backup.BackupType),
			Created:       types.StringValue(backup.Created),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
