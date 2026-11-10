package dns

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ datasource.DataSourceWithConfigure        = (*domainDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*domainDataSource)(nil)
)

// NewDomainDataSource returns the onetsolutions_domain data source.
func NewDomainDataSource() datasource.DataSource {
	return &domainDataSource{}
}

type domainDataSource struct {
	common.DataSourceBase
}

type domainDataModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	ProjectID        types.String `tfsdk:"project_id"`
	Name             types.String `tfsdk:"name"`
	Status           types.String `tfsdk:"status"`
	RegistrationDate types.String `tfsdk:"registration_date"`
	ExpirationDate   types.String `tfsdk:"expiration_date"`
	AutoRenew        types.Bool   `tfsdk:"auto_renew"`
	Locked           types.Bool   `tfsdk:"locked"`
	WhoisPrivacy     types.Bool   `tfsdk:"whois_privacy"`
	Nameservers      types.List   `tfsdk:"nameservers"`
	DNSZoneID        types.String `tfsdk:"dns_zone_id"`
	RecurringAmount  types.String `tfsdk:"recurring_amount"`
	NextBillingDate  types.String `tfsdk:"next_billing_date"`
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	computedBool := func(description string) schema.BoolAttribute {
		return schema.BoolAttribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a domain registered in a project by `id` or by `name`.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the domain. Exactly one of `id` and `name` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The domain name, such as `example.fr`. Exactly one of `id` and `name` is required.",
			},
			"status":            computed("The registration status, such as `active`, `expiring_soon` or `expired`."),
			"registration_date": computed("When the domain was registered (YYYY-MM-DD)."),
			"expiration_date":   computed("When the registration expires (YYYY-MM-DD)."),
			"auto_renew":        computedBool("Whether the domain renews automatically."),
			"locked":            computedBool("Whether the domain is locked at the registry against transfers."),
			"whois_privacy":     computedBool("Whether the registrant's contact details are hidden from WHOIS."),
			"nameservers": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The name servers the domain is delegated to.",
			},
			"dns_zone_id":       computed("The UUID of the DNS zone attached to the domain, when there is one."),
			"recurring_amount":  computed("The amount billed at each renewal, in EUR, as a decimal string."),
			"next_billing_date": computed("The next renewal billing date (YYYY-MM-DD)."),
		}),
	}
}

func (d *domainDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainDataModel
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
		domains, err := c.ListDomains(ctx, scope)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the domains", err)
			return
		}
		name := config.Name.ValueString()
		found, findDiags := common.FindOne(domains,
			func(dm client.DomainSummary) bool { return dm.Name == name && dm.IsActive && dm.Status != "deleted" },
			func(dm client.DomainSummary) string { return dm.ID },
			"domain", fmt.Sprintf("name %q", name))
		resp.Diagnostics.Append(findDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		id = found.ID
	}
	domain, err := c.GetDomain(ctx, scope, id)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the domain", err)
		return
	}
	model := domainDataModel{
		ID:               types.StringValue(domain.ID),
		OrganizationID:   types.StringValue(scope.OrganizationID),
		ProjectID:        types.StringValue(scope.ProjectID),
		Name:             types.StringValue(domain.Name),
		Status:           types.StringValue(domain.Status),
		RegistrationDate: types.StringPointerValue(domain.RegistrationDate),
		ExpirationDate:   types.StringPointerValue(domain.ExpirationDate),
		AutoRenew:        types.BoolValue(domain.AutoRenew),
		Locked:           types.BoolValue(domain.IsLocked),
		WhoisPrivacy:     types.BoolValue(domain.WhoisPrivacy),
		Nameservers:      stringListValue(ctx, domain.NameserverList()),
		DNSZoneID:        types.StringNull(),
		RecurringAmount:  types.StringPointerValue(domain.RecurringAmount),
		NextBillingDate:  types.StringPointerValue(domain.NextBillingDate),
	}
	if domain.DNSZone != nil {
		model.DNSZoneID = types.StringValue(domain.DNSZone.ID)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
