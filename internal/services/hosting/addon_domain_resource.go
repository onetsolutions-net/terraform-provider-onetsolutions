package hosting

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*addonDomainResource)(nil)
	_ resource.ResourceWithImportState = (*addonDomainResource)(nil)
)

var addonSubdomainPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// NewAddonDomainResource returns the onetsolutions_hosting_addon_domain resource.
func NewAddonDomainResource() resource.Resource {
	return &addonDomainResource{}
}

type addonDomainResource struct {
	common.ResourceBase
}

type addonDomainModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Domain         types.String `tfsdk:"domain"`
	Subdomain      types.String `tfsdk:"subdomain"`
	DocumentRoot   types.String `tfsdk:"document_root"`
}

// fill copies a listed addon domain, keeping the spelling of the state when it designates the
// same subdomain and directory.
func (m *addonDomainModel) fill(d *client.HostingAddonDomain) {
	domain := strings.ToLower(d.Domain)
	m.ID = types.StringValue(domain)
	m.Domain = types.StringValue(domain)
	if m.Subdomain.IsNull() || m.Subdomain.IsUnknown() || !strings.EqualFold(m.Subdomain.ValueString(), d.Subdomain) {
		m.Subdomain = types.StringValue(d.Subdomain)
	}
	if m.DocumentRoot.IsNull() || m.DocumentRoot.IsUnknown() || !sameDirectory(m.DocumentRoot.ValueString(), d.DocumentRoot) {
		m.DocumentRoot = types.StringValue(d.DocumentRoot)
	}
}

func findAddonDomain(domains []client.HostingAddonDomain, domain string) *client.HostingAddonDomain {
	for i := range domains {
		if strings.EqualFold(domains[i].Domain, domain) {
			return &domains[i]
		}
	}
	return nil
}

func (r *addonDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_addon_domain"
}

func (r *addonDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an addon domain of a web hosting: another domain served by the same account, from its own directory.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The addon domain, same as `domain`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			attrHostingID: hostingIDAttribute("addon domain"),
			"domain": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The domain to serve, such as `example.org`: lowercase, without a trailing dot. Its " +
					"DNS must point at the web hosting for the site to be reachable. Changing it recreates the addon domain.",
				Validators:    webDomainValidators(),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"subdomain": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The internal subdomain of the main domain the addon domain is attached to: letters, " +
					"digits and hyphens, without dots, 63 characters at most. When unset, derived from the domain " +
					"(`example-org` for `example.org`). Changing it recreates the addon domain.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(addonSubdomainPattern, "must be letters, digits and hyphens only, without dots, 63 characters at most"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"document_root": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The directory the domain serves, relative to the account's home directory, such as " +
					"`public_html/example.org`. When unset, the web hosting's default directory for the domain; the state " +
					"then holds the absolute path the web hosting reports. Changing it recreates the addon domain.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1), common.NoSurroundingWhitespace()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
		}),
	}
}

func (r *addonDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan addonDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := r.ResolveScope(ctx, plan.OrganizationID, plan.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.OrganizationID, plan.ProjectID = types.StringValue(scope.OrganizationID), types.StringValue(scope.ProjectID)
	hostingID, domain := plan.HostingID.ValueString(), plan.Domain.ValueString()

	existing, err := c.ListHostingAddonDomains(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the addon domains of the web hosting", err)
		return
	}
	if findAddonDomain(existing, domain) != nil {
		resp.Diagnostics.AddAttributeError(path.Root("domain"), "Addon domain already exists",
			fmt.Sprintf("The web hosting already serves %s. Import it rather than creating it again.", domain))
		return
	}
	created, err := c.CreateHostingAddonDomain(ctx, scope, hostingID, client.HostingAddonDomainInput{
		Domain: domain, Subdomain: plan.Subdomain.ValueString(), DocumentRoot: plan.DocumentRoot.ValueString(),
	})
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the addon domain", err, map[string]path.Path{
			"domain": path.Root("domain"), "subdomain": path.Root("subdomain"), "document_root": path.Root("document_root"),
		})
		return
	}
	listed, err := c.ListHostingAddonDomains(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the addon domains of the web hosting", err)
		return
	}
	if found := findAddonDomain(listed, domain); found != nil {
		created = found
	}
	plan.fill(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *addonDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state addonDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	domains, gone, err := webChildList(ctx, c, webScope(state.OrganizationID, state.ProjectID), state.HostingID.ValueString(), c.ListHostingAddonDomains)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the addon domains of the web hosting", err)
		return
	}
	found := findAddonDomain(domains, state.ID.ValueString())
	if gone || found == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(found)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called: every argument forces a replacement.
func (r *addonDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan addonDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *addonDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state addonDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID := webScope(state.OrganizationID, state.ProjectID), state.HostingID.ValueString()
	domains, gone, err := webChildList(ctx, c, scope, hostingID, c.ListHostingAddonDomains)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the addon domains of the web hosting", err)
		return
	}
	found := findAddonDomain(domains, state.ID.ValueString())
	if gone || found == nil {
		return
	}
	if err := c.DeleteHostingAddonDomain(ctx, scope, hostingID, found.Domain); err != nil {
		webChildDeleteFailed(ctx, c, scope, hostingID, err, &resp.Diagnostics, "Could not delete the addon domain")
	}
}

func (r *addonDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importWebChild(ctx, &r.ResourceBase, req, resp, "domain")
}
