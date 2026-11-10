package account

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ datasource.DataSourceWithConfigure        = (*projectDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*projectDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*projectsDataSource)(nil)
)

type projectModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
	DomainQuota    types.Int64  `tfsdk:"domain_quota"`
}

type projectItemModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	DomainQuota types.Int64  `tfsdk:"domain_quota"`
}

var projectAttrTypes = map[string]attr.Type{
	"id":           types.StringType,
	"name":         types.StringType,
	"domain_quota": types.Int64Type,
}

const domainQuotaDescription = "The number of domains the project may hold."

// NewProjectDataSource returns the onetsolutions_project data source.
func NewProjectDataSource() datasource.DataSource {
	return &projectDataSource{}
}

type projectDataSource struct {
	common.DataSourceBase
}

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a project of an organization by `id` or by `name`.",
		Attributes: map[string]schema.Attribute{
			common.AttrOrganizationID: common.DataSourceOrganizationAttribute(),
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the project. Exactly one of `id` and `name` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The name of the project. Exactly one of `id` and `name` is required.",
			},
			"domain_quota": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: domainQuotaDescription,
			},
		},
	}
}

func (d *projectDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config projectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	org, diags := d.ResolveOrganization(ctx, config.OrganizationID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var project *client.Project
	if !config.ID.IsNull() {
		found, err := c.GetProject(ctx, org, config.ID.ValueString())
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the project", err)
			return
		}
		project = found
	} else {
		projects, err := c.ListProjects(ctx, org)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the projects", err)
			return
		}
		name := config.Name.ValueString()
		found, diags := common.FindOne(projects,
			func(p client.Project) bool { return p.Name == name },
			func(p client.Project) string { return p.ID },
			"project", fmt.Sprintf("name %q in organization %s", name, org))
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		project = &found
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, projectModel{
		ID:             types.StringValue(project.ID),
		OrganizationID: types.StringValue(org),
		Name:           types.StringValue(project.Name),
		DomainQuota:    types.Int64Value(project.DomainQuota),
	})...)
}

// NewProjectsDataSource returns the onetsolutions_projects data source.
func NewProjectsDataSource() datasource.DataSource {
	return &projectsDataSource{}
}

type projectsDataSource struct {
	common.DataSourceBase
}

type projectsModel struct {
	OrganizationID types.String `tfsdk:"organization_id"`
	Projects       types.List   `tfsdk:"projects"`
}

func (d *projectsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_projects"
}

func (d *projectsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the projects of an organization the API key's user can access.",
		Attributes: map[string]schema.Attribute{
			common.AttrOrganizationID: common.DataSourceOrganizationAttribute(),
			"projects": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The projects.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The UUID of the project.",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The name of the project.",
						},
						"domain_quota": schema.Int64Attribute{
							Computed:            true,
							MarkdownDescription: domainQuotaDescription,
						},
					},
				},
			},
		},
	}
}

func (d *projectsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config projectsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	org, diags := d.ResolveOrganization(ctx, config.OrganizationID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	projects, err := c.ListProjects(ctx, org)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the projects", err)
		return
	}
	items := make([]projectItemModel, 0, len(projects))
	for _, project := range projects {
		items = append(items, projectItemModel{
			ID:          types.StringValue(project.ID),
			Name:        types.StringValue(project.Name),
			DomainQuota: types.Int64Value(project.DomainQuota),
		})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: projectAttrTypes}, items)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, projectsModel{
		OrganizationID: types.StringValue(org),
		Projects:       list,
	})...)
}
