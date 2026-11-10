package catalog

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
	_ datasource.DataSourceWithConfigure        = (*hostingPlansDataSource)(nil)
	_ datasource.DataSourceWithConfigure        = (*webHostingDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*webHostingDataSource)(nil)
)

// NewHostingPlansDataSource returns the onetsolutions_hosting_plans data source.
func NewHostingPlansDataSource() datasource.DataSource {
	return &hostingPlansDataSource{}
}

type hostingPlansDataSource struct {
	common.DataSourceBase
}

type hostingPlanModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Slug             types.String `tfsdk:"slug"`
	Description      types.String `tfsdk:"description"`
	HostingType      types.String `tfsdk:"hosting_type"`
	DiskSpaceMB      types.Int64  `tfsdk:"disk_space_mb"`
	BandwidthMB      types.Int64  `tfsdk:"bandwidth_mb"`
	MaxEmailAccounts types.Int64  `tfsdk:"max_email_accounts"`
	MaxDatabases     types.Int64  `tfsdk:"max_databases"`
	MaxSubdomains    types.Int64  `tfsdk:"max_subdomains"`
	MaxAddonDomains  types.Int64  `tfsdk:"max_addon_domains"`
	MaxFTPAccounts   types.Int64  `tfsdk:"max_ftp_accounts"`
	Pricing          types.List   `tfsdk:"pricing"`
}

var hostingPlanAttrTypes = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "slug": types.StringType, "description": types.StringType,
	"hosting_type": types.StringType, "disk_space_mb": types.Int64Type, "bandwidth_mb": types.Int64Type,
	"max_email_accounts": types.Int64Type, "max_databases": types.Int64Type, "max_subdomains": types.Int64Type,
	"max_addon_domains": types.Int64Type, "max_ftp_accounts": types.Int64Type,
	"pricing": types.ListType{ElemType: types.ObjectType{AttrTypes: sizePricingAttrTypes}},
}

type hostingPlansModel struct {
	Plans types.List `tfsdk:"plans"`
}

func (d *hostingPlansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_plans"
}

func limit(what string) string {
	return what + "; 0 means unlimited."
}

func (d *hostingPlansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the web hosting plans on sale.",
		Attributes: map[string]schema.Attribute{
			"plans": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The plans.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":                 schema.StringAttribute{Computed: true, MarkdownDescription: "The UUID of the plan."},
					"name":               schema.StringAttribute{Computed: true, MarkdownDescription: "The name of the plan."},
					"slug":               schema.StringAttribute{Computed: true, MarkdownDescription: "The slug of the plan."},
					"description":        schema.StringAttribute{Computed: true, MarkdownDescription: "The description of the plan (Markdown)."},
					"hosting_type":       schema.StringAttribute{Computed: true, MarkdownDescription: "`hostingaccount` (standard) or `reselleraccount` (reseller)."},
					"disk_space_mb":      schema.Int64Attribute{Computed: true, MarkdownDescription: limit("The disk space, in MB")},
					"bandwidth_mb":       schema.Int64Attribute{Computed: true, MarkdownDescription: limit("The monthly bandwidth, in MB")},
					"max_email_accounts": schema.Int64Attribute{Computed: true, MarkdownDescription: limit("The most e-mail accounts")},
					"max_databases":      schema.Int64Attribute{Computed: true, MarkdownDescription: limit("The most databases")},
					"max_subdomains":     schema.Int64Attribute{Computed: true, MarkdownDescription: limit("The most subdomains")},
					"max_addon_domains":  schema.Int64Attribute{Computed: true, MarkdownDescription: limit("The most addon domains")},
					"max_ftp_accounts":   schema.Int64Attribute{Computed: true, MarkdownDescription: limit("The most FTP accounts")},
					"pricing": schema.ListNestedAttribute{
						Computed:            true,
						MarkdownDescription: "The price of the plan for each billing cycle, excluding VAT.",
						NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
							"billing_cycle":    schema.StringAttribute{Computed: true, MarkdownDescription: "The billing cycle, such as `monthly`."},
							"price":            schema.StringAttribute{Computed: true, MarkdownDescription: "The price per cycle, in EUR, as a decimal string."},
							"discount_percent": schema.StringAttribute{Computed: true, MarkdownDescription: "The discount of the cycle, in percent."},
							"is_default":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether it is the plan's default cycle."},
						}},
					},
				}},
			},
		},
	}
}

func (d *hostingPlansDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plans, err := c.ListHostingPlans(ctx)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the hosting plans", err)
		return
	}
	models := make([]hostingPlanModel, 0, len(plans))
	for i := range plans {
		p := &plans[i]
		if !p.IsActive {
			continue
		}
		pricing := make([]sizePricingModel, 0, len(p.Pricing))
		for _, price := range p.Pricing {
			pricing = append(pricing, sizePricingModel{
				BillingCycle: types.StringValue(price.BillingCycle), Price: types.StringValue(price.Price),
				DiscountPercent: types.StringValue(price.DiscountPercent), IsDefault: types.BoolValue(price.IsDefault),
			})
		}
		pricingList, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: sizePricingAttrTypes}, pricing)
		resp.Diagnostics.Append(diags...)
		models = append(models, hostingPlanModel{
			ID: types.StringValue(p.ID), Name: types.StringValue(p.Name), Slug: types.StringValue(p.Slug),
			Description: types.StringValue(p.Description), HostingType: types.StringValue(p.HostingType),
			DiskSpaceMB: types.Int64Value(p.DiskSpaceMB), BandwidthMB: types.Int64Value(p.BandwidthMB),
			MaxEmailAccounts: types.Int64Value(p.MaxEmailAccounts), MaxDatabases: types.Int64Value(p.MaxDatabases),
			MaxSubdomains: types.Int64Value(p.MaxSubdomains), MaxAddonDomains: types.Int64Value(p.MaxAddonDomains),
			MaxFTPAccounts: types.Int64Value(p.MaxFTPAccounts), Pricing: pricingList,
		})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: hostingPlanAttrTypes}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, hostingPlansModel{Plans: list})...)
}

// NewWebHostingDataSource returns the onetsolutions_web_hosting data source.
func NewWebHostingDataSource() datasource.DataSource {
	return &webHostingDataSource{}
}

type webHostingDataSource struct {
	common.DataSourceBase
}

type webHostingModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	ProjectID        types.String `tfsdk:"project_id"`
	Domain           types.String `tfsdk:"domain"`
	Username         types.String `tfsdk:"username"`
	Status           types.String `tfsdk:"status"`
	PlanID           types.String `tfsdk:"plan_id"`
	PlanName         types.String `tfsdk:"plan_name"`
	DiskUsageMB      types.Int64  `tfsdk:"disk_usage_mb"`
	DiskLimitMB      types.Int64  `tfsdk:"disk_limit_mb"`
	BandwidthUsageMB types.Int64  `tfsdk:"bandwidth_usage_mb"`
	BandwidthLimitMB types.Int64  `tfsdk:"bandwidth_limit_mb"`
	DedicatedIP      types.String `tfsdk:"dedicated_ip"`
	Nameservers      types.List   `tfsdk:"nameservers"`
	BillingCycle     types.String `tfsdk:"billing_cycle"`
	Price            types.String `tfsdk:"price"`
	NextBillingDate  types.String `tfsdk:"next_billing_date"`
	Created          types.String `tfsdk:"created"`
}

func (d *webHostingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_web_hosting"
}

func (d *webHostingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	computedInt := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a web hosting of a project by `id` or by `domain`.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the web hosting. Exactly one of `id` and `domain` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"domain": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The primary domain of the web hosting. Exactly one of `id` and `domain` is required.",
			},
			"username":           computed("The account's login name."),
			"status":             computed("`pending`, `active`, `suspended`, `terminated`, `cancelled` or `failed`."),
			"plan_id":            computed("The UUID of the plan (see `onetsolutions_hosting_plans`)."),
			"plan_name":          computed("The name of the plan."),
			"disk_usage_mb":      computedInt("The disk space used, in MB."),
			"disk_limit_mb":      computedInt("The disk space allowed, in MB."),
			"bandwidth_usage_mb": computedInt("The bandwidth used this month, in MB."),
			"bandwidth_limit_mb": computedInt("The monthly bandwidth allowed, in MB; 0 means unlimited."),
			"dedicated_ip":       computed("The dedicated IP address, when one is assigned."),
			"nameservers": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The name servers to delegate the domain to.",
			},
			"billing_cycle":     computed("The billing cycle, such as `monthly`."),
			"price":             computed("The price per cycle, in EUR, as a decimal string."),
			"next_billing_date": computed("The next billing date (RFC 3339), when one is scheduled."),
			"created":           computed("When the web hosting was created (RFC 3339)."),
		}),
	}
}

func (d *webHostingDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("domain")),
	}
}

func (d *webHostingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config webHostingModel
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
	id := config.ID.ValueString()
	if config.ID.IsNull() {
		hostings, err := c.ListWebHostings(ctx, scope)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the web hostings", err)
			return
		}
		domain := config.Domain.ValueString()
		found, findDiags := common.FindOne(hostings,
			func(h client.WebHostingSummary) bool {
				return h.Domain == domain && h.Status != "terminated" && h.Status != "cancelled"
			},
			func(h client.WebHostingSummary) string { return h.ID },
			"web hosting", fmt.Sprintf("domain %q", domain))
		resp.Diagnostics.Append(findDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		id = found.ID
	}
	hosting, err := c.GetWebHosting(ctx, scope, id)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the web hosting", err)
		return
	}
	nameservers := []string{}
	for _, ns := range []string{hosting.NS1, hosting.NS2} {
		if ns != "" {
			nameservers = append(nameservers, ns)
		}
	}
	nsList, diags := types.ListValueFrom(ctx, types.StringType, nameservers)
	resp.Diagnostics.Append(diags...)
	model := webHostingModel{
		ID:               types.StringValue(hosting.ID),
		OrganizationID:   types.StringValue(scope.OrganizationID),
		ProjectID:        types.StringValue(scope.ProjectID),
		Domain:           types.StringValue(hosting.Domain),
		Username:         types.StringValue(hosting.Username),
		Status:           types.StringValue(hosting.Status),
		PlanID:           types.StringNull(),
		PlanName:         types.StringNull(),
		DiskUsageMB:      types.Int64Value(hosting.DiskUsageMB),
		DiskLimitMB:      types.Int64Value(hosting.DiskLimitMB),
		BandwidthUsageMB: types.Int64Value(hosting.BandwidthUsageMB),
		BandwidthLimitMB: types.Int64Value(hosting.BandwidthLimitMB),
		DedicatedIP:      types.StringPointerValue(hosting.DedicatedIP),
		Nameservers:      nsList,
		BillingCycle:     types.StringValue(hosting.BillingCycle),
		Price:            types.StringValue(hosting.Price),
		NextBillingDate:  types.StringPointerValue(hosting.NextBillingDate),
		Created:          types.StringValue(hosting.Created),
	}
	if hosting.Plan != nil {
		model.PlanID = types.StringValue(hosting.Plan.ID)
		model.PlanName = types.StringValue(hosting.Plan.Name)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
