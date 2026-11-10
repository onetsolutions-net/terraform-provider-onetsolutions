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
	_ datasource.DataSourceWithConfigure        = (*organizationDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*organizationDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*organizationsDataSource)(nil)
)

type organizationModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	OrganizationType types.String `tfsdk:"organization_type"`
	SupportPlan      types.String `tfsdk:"support_plan"`
	CompanyName      types.String `tfsdk:"company_name"`
	Country          types.String `tfsdk:"country"`
	ProjectCount     types.Int64  `tfsdk:"project_count"`
	IsActive         types.Bool   `tfsdk:"is_active"`
	Created          types.String `tfsdk:"created"`
}

func newOrganizationModel(org *client.Organization) organizationModel {
	return organizationModel{
		ID:               types.StringValue(org.ID),
		Name:             types.StringValue(org.Name),
		OrganizationType: types.StringValue(org.OrganizationType),
		SupportPlan:      types.StringValue(org.SupportPlan),
		CompanyName:      types.StringValue(org.CompanyName),
		Country:          types.StringValue(org.Country),
		ProjectCount:     types.Int64Value(org.ProjectCount),
		IsActive:         types.BoolValue(org.IsActive),
		Created:          types.StringValue(org.Created),
	}
}

var organizationAttrTypes = map[string]attr.Type{
	"id":                types.StringType,
	"name":              types.StringType,
	"organization_type": types.StringType,
	"support_plan":      types.StringType,
	"company_name":      types.StringType,
	"country":           types.StringType,
	"project_count":     types.Int64Type,
	"is_active":         types.BoolType,
	"created":           types.StringType,
}

// organizationComputedAttributes are the attributes both organization data sources return.
func organizationComputedAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"organization_type": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "`personal` or `professional`.",
		},
		"support_plan": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The support plan: `basic`, `advanced` or `business`.",
		},
		"company_name": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The company name on invoices.",
		},
		"country": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The ISO 3166-1 alpha-2 country code of the billing address.",
		},
		"project_count": schema.Int64Attribute{
			Computed:            true,
			MarkdownDescription: "The number of projects in the organization.",
		},
		"is_active": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether the organization is active.",
		},
		"created": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "When the organization was created (RFC 3339).",
		},
	}
}

// NewOrganizationDataSource returns the onetsolutions_organization data source.
func NewOrganizationDataSource() datasource.DataSource {
	return &organizationDataSource{}
}

type organizationDataSource struct {
	common.DataSourceBase
}

func (d *organizationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (d *organizationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an organization the API key's user belongs to, by `id` or by `name`.",
		Attributes: common.MergeAttributes(organizationComputedAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the organization. Exactly one of `id` and `name` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The name of the organization. Exactly one of `id` and `name` is required.",
			},
		}),
	}
}

func (d *organizationDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *organizationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config organizationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var org *client.Organization
	if !config.ID.IsNull() {
		found, err := c.GetOrganization(ctx, config.ID.ValueString())
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the organization", err)
			return
		}
		org = found
	} else {
		orgs, err := c.ListOrganizations(ctx)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the organizations", err)
			return
		}
		name := config.Name.ValueString()
		found, diags := common.FindOne(orgs,
			func(o client.Organization) bool { return o.Name == name },
			func(o client.Organization) string { return o.ID },
			"organization", fmt.Sprintf("name %q", name))
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		org = &found
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, newOrganizationModel(org))...)
}

// NewOrganizationsDataSource returns the onetsolutions_organizations data source.
func NewOrganizationsDataSource() datasource.DataSource {
	return &organizationsDataSource{}
}

type organizationsDataSource struct {
	common.DataSourceBase
}

type organizationsModel struct {
	Organizations types.List `tfsdk:"organizations"`
}

func (d *organizationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organizations"
}

func (d *organizationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the organizations the API key's user belongs to, sorted by name.",
		Attributes: map[string]schema.Attribute{
			"organizations": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The organizations.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: common.MergeAttributes(organizationComputedAttributes(), map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The UUID of the organization.",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "The name of the organization.",
						},
					}),
				},
			},
		},
	}
}

func (d *organizationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	orgs, err := c.ListOrganizations(ctx)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the organizations", err)
		return
	}
	models := make([]organizationModel, 0, len(orgs))
	for i := range orgs {
		models = append(models, newOrganizationModel(&orgs[i]))
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: organizationAttrTypes}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, organizationsModel{Organizations: list})...)
}
