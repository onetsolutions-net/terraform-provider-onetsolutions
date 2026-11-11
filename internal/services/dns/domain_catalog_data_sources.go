package dns

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ datasource.DataSourceWithConfigure = (*domainAvailabilityDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*tldsDataSource)(nil)
)

// NewDomainAvailabilityDataSource returns the onetsolutions_domain_availability data source.
func NewDomainAvailabilityDataSource() datasource.DataSource {
	return &domainAvailabilityDataSource{}
}

type domainAvailabilityDataSource struct {
	common.DataSourceBase
}

type domainAvailabilityModel struct {
	Name        types.String `tfsdk:"name"`
	Available   types.Bool   `tfsdk:"available"`
	TLD         types.String `tfsdk:"tld"`
	Price       types.String `tfsdk:"price"`
	Suggestions types.List   `tfsdk:"suggestions"`
}

type domainSuggestionModel struct {
	Name  types.String `tfsdk:"name"`
	TLD   types.String `tfsdk:"tld"`
	Price types.String `tfsdk:"price"`
}

var domainSuggestionAttrTypes = map[string]attr.Type{
	"name": types.StringType, "tld": types.StringType, "price": types.StringType,
}

func (d *domainAvailabilityDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_availability"
}

func (d *domainAvailabilityDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Checks whether a domain name can be registered, and at what price.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The domain name to check, lowercase without a trailing dot, such as `example.com`.",
				Validators:          []validator.String{domainNameValidator{}},
			},
			"available": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the name can be registered.",
			},
			"tld": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The extension of the name, such as `com` or `co.uk`.",
			},
			"price": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The price of the first year of registration, in EUR, as a decimal string; null when the name is not available.",
			},
			"suggestions": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Available names close to `name`, when it is not available.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name":  schema.StringAttribute{Computed: true, MarkdownDescription: "The domain name."},
					"tld":   schema.StringAttribute{Computed: true, MarkdownDescription: "Its extension."},
					"price": schema.StringAttribute{Computed: true, MarkdownDescription: "The price of its first year, in EUR, as a decimal string."},
				}},
			},
		},
	}
}

func (d *domainAvailabilityDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config domainAvailabilityModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	result, err := c.CheckDomainAvailability(ctx, config.Name.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not check the domain", err)
		return
	}
	suggestions := make([]domainSuggestionModel, 0, len(result.Suggestions))
	for _, s := range result.Suggestions {
		if !s.Available {
			continue
		}
		suggestions = append(suggestions, domainSuggestionModel{
			Name: types.StringValue(s.DomainName), TLD: types.StringValue(s.TLD), Price: types.StringPointerValue(s.Price),
		})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: domainSuggestionAttrTypes}, suggestions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, domainAvailabilityModel{
		Name:        config.Name,
		Available:   types.BoolValue(result.Available),
		TLD:         types.StringValue(result.TLD),
		Price:       types.StringPointerValue(result.Price),
		Suggestions: list,
	})...)
}

// NewTLDsDataSource returns the onetsolutions_tlds data source.
func NewTLDsDataSource() datasource.DataSource {
	return &tldsDataSource{}
}

type tldsDataSource struct {
	common.DataSourceBase
}

type tldsModel struct {
	TLD                   types.String `tfsdk:"tld"`
	Category              types.String `tfsdk:"category"`
	RegistrationAvailable types.Bool   `tfsdk:"registration_available"`
	TLDs                  types.List   `tfsdk:"tlds"`
}

type tldModel struct {
	TLD                   types.String `tfsdk:"tld"`
	DisplayName           types.String `tfsdk:"display_name"`
	Category              types.String `tfsdk:"category"`
	RegistrationAvailable types.Bool   `tfsdk:"registration_available"`
	TransferAvailable     types.Bool   `tfsdk:"transfer_available"`
	WhoisPrivacyAvailable types.Bool   `tfsdk:"whois_privacy_available"`
	WhoisPrivacyPrice     types.String `tfsdk:"whois_privacy_price"`
	TransferLockAvailable types.Bool   `tfsdk:"transfer_lock_available"`
	MinRegistrationYears  types.Int64  `tfsdk:"min_registration_years"`
	MaxRegistrationYears  types.Int64  `tfsdk:"max_registration_years"`
	Pricing               types.List   `tfsdk:"pricing"`
}

type tldPricingModel struct {
	Years             types.Int64  `tfsdk:"years"`
	RegistrationPrice types.String `tfsdk:"registration_price"`
	RenewalPrice      types.String `tfsdk:"renewal_price"`
	TransferPrice     types.String `tfsdk:"transfer_price"`
}

var tldPricingAttrTypes = map[string]attr.Type{
	"years": types.Int64Type, "registration_price": types.StringType, "renewal_price": types.StringType,
	"transfer_price": types.StringType,
}

var tldAttrTypes = map[string]attr.Type{
	"tld": types.StringType, "display_name": types.StringType, "category": types.StringType,
	"registration_available": types.BoolType, "transfer_available": types.BoolType,
	"whois_privacy_available": types.BoolType, "whois_privacy_price": types.StringType,
	"transfer_lock_available": types.BoolType, "min_registration_years": types.Int64Type,
	"max_registration_years": types.Int64Type,
	"pricing":                types.ListType{ElemType: types.ObjectType{AttrTypes: tldPricingAttrTypes}},
}

func (d *tldsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tlds"
}

func (d *tldsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: description}
	}
	boolean := func(description string) schema.BoolAttribute {
		return schema.BoolAttribute{Computed: true, MarkdownDescription: description}
	}
	integer := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Computed: true, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the domain extensions on sale, with their prices.",
		Attributes: map[string]schema.Attribute{
			"tld": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only the extension with this name, without its leading dot, such as `fr`.",
			},
			"category": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only the extensions of this category: `generic`, `country` or `new`.",
				Validators:          []validator.String{stringvalidator.OneOf("generic", "country", "new")},
			},
			"registration_available": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "When set, only the extensions whose registration is (`true`) or is not (`false`) open.",
			},
			"tlds": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The extensions.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"tld":                     str("The extension, without its leading dot, such as `com`."),
					"display_name":            str("Its display name."),
					"category":                str("`generic`, `country`, `new` or `premium`."),
					"registration_available":  boolean("Whether new names can be registered in it."),
					"transfer_available":      boolean("Whether its domains can be transferred in."),
					"whois_privacy_available": boolean("Whether WHOIS privacy can be enabled on its domains."),
					"whois_privacy_price":     str("The price of WHOIS privacy, in EUR, as a decimal string."),
					"transfer_lock_available": boolean("Whether its domains can be locked against transfers."),
					"min_registration_years":  integer("The shortest registration length, in years."),
					"max_registration_years":  integer("The longest registration length, in years."),
					"pricing": schema.ListNestedAttribute{
						Computed:            true,
						MarkdownDescription: "Its prices per registration length, in EUR, as decimal strings.",
						NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
							"years":              integer("The registration length, in years."),
							"registration_price": str("The price of a registration."),
							"renewal_price":      str("The price of a renewal."),
							"transfer_price":     str("The price of a transfer in."),
						}},
					},
				}},
			},
		},
	}
}

func (d *tldsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config tldsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	c := d.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	filter := client.TLDFilter{TLD: config.TLD.ValueString(), Category: config.Category.ValueString()}
	if common.Known(config.RegistrationAvailable) {
		filter.IsRegistrationAvailable = config.RegistrationAvailable.ValueBoolPointer()
	}
	tlds, err := c.ListTLDs(ctx, filter)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the domain extensions", err)
		return
	}
	models := make([]tldModel, 0, len(tlds))
	for i := range tlds {
		tld := &tlds[i]
		pricing := make([]tldPricingModel, 0, len(tld.Pricing))
		for _, p := range tld.Pricing {
			pricing = append(pricing, tldPricingModel{
				Years: types.Int64Value(p.Years), RegistrationPrice: types.StringValue(p.RegistrationPrice),
				RenewalPrice: types.StringValue(p.RenewalPrice), TransferPrice: types.StringValue(p.TransferPrice),
			})
		}
		pricingList, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: tldPricingAttrTypes}, pricing)
		resp.Diagnostics.Append(diags...)
		models = append(models, tldModel{
			TLD: types.StringValue(tld.TLD), DisplayName: types.StringValue(tld.DisplayName),
			Category: types.StringValue(tld.Category), RegistrationAvailable: types.BoolValue(tld.IsRegistrationAvailable),
			TransferAvailable: types.BoolValue(tld.IsTransferAvailable), WhoisPrivacyAvailable: types.BoolValue(tld.WhoisPrivacyAvailable),
			WhoisPrivacyPrice: types.StringPointerValue(tld.WhoisPrivacyPrice), TransferLockAvailable: types.BoolValue(tld.TransferLockAvailable),
			MinRegistrationYears: types.Int64Value(tld.MinRegistrationYears), MaxRegistrationYears: types.Int64Value(tld.MaxRegistrationYears),
			Pricing: pricingList,
		})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: tldAttrTypes}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.TLDs = list
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
