package hosting

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var _ datasource.DataSourceWithConfigure = (*phpVersionsDataSource)(nil)

// NewPHPVersionsDataSource returns the onetsolutions_hosting_php_versions data source.
func NewPHPVersionsDataSource() datasource.DataSource {
	return &phpVersionsDataSource{}
}

type phpVersionsDataSource struct {
	common.DataSourceBase
}

type phpVersionsModel struct {
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Versions       types.List   `tfsdk:"versions"`
	Domains        types.List   `tfsdk:"domains"`
}

var (
	phpVersionAttrTypes       = map[string]attr.Type{"version": types.StringType, "display": types.StringType}
	domainPHPVersionAttrTypes = map[string]attr.Type{"domain": types.StringType, "php_version": types.StringType}
)

func (ds *phpVersionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_php_versions"
}

func (ds *phpVersionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the PHP versions a web hosting offers and the version each of its domains runs.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"hosting_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the web hosting.",
				Validators:          []validator.String{common.UUID()},
			},
			"versions": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The PHP versions the web hosting offers.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"version": schema.StringAttribute{Computed: true, MarkdownDescription: "The version, as `php_version` of `onetsolutions_hosting_php_version` takes it."},
					"display": schema.StringAttribute{Computed: true, MarkdownDescription: "A readable name of the version."},
				}},
			},
			"domains": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The domains of the web hosting, with the PHP version each runs.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"domain":      schema.StringAttribute{Computed: true, MarkdownDescription: "The domain."},
					"php_version": schema.StringAttribute{Computed: true, MarkdownDescription: "Its PHP version, `inherit` when it runs the account's default."},
				}},
			},
		}),
	}
}

func (ds *phpVersionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config phpVersionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := ds.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := ds.ResolveScope(ctx, config.OrganizationID, config.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	hostingID := config.HostingID.ValueString()
	versions, err := c.ListHostingPHPVersions(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the PHP versions of the web hosting", err)
		return
	}
	domains, err := c.ListHostingDomainPHPVersions(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the PHP versions of the domains", err)
		return
	}
	versionValues := make([]attr.Value, 0, len(versions))
	for _, v := range versions {
		object, d := types.ObjectValue(phpVersionAttrTypes, map[string]attr.Value{
			"version": types.StringValue(v.Version), "display": types.StringValue(v.Display),
		})
		resp.Diagnostics.Append(d...)
		versionValues = append(versionValues, object)
	}
	domainValues := make([]attr.Value, 0, len(domains))
	for _, v := range domains {
		object, d := types.ObjectValue(domainPHPVersionAttrTypes, map[string]attr.Value{
			"domain": types.StringValue(v.Domain), "php_version": types.StringValue(v.PHPVersion),
		})
		resp.Diagnostics.Append(d...)
		domainValues = append(domainValues, object)
	}
	versionList, listDiags := types.ListValue(types.ObjectType{AttrTypes: phpVersionAttrTypes}, versionValues)
	resp.Diagnostics.Append(listDiags...)
	domainList, listDiags := types.ListValue(types.ObjectType{AttrTypes: domainPHPVersionAttrTypes}, domainValues)
	resp.Diagnostics.Append(listDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.OrganizationID = types.StringValue(scope.OrganizationID)
	config.ProjectID = types.StringValue(scope.ProjectID)
	config.Versions = versionList
	config.Domains = domainList
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
