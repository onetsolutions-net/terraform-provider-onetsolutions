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
	_ resource.ResourceWithConfigure   = (*subdomainResource)(nil)
	_ resource.ResourceWithImportState = (*subdomainResource)(nil)
)

var subdomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

// NewSubdomainResource returns the onetsolutions_hosting_subdomain resource.
func NewSubdomainResource() resource.Resource {
	return &subdomainResource{}
}

type subdomainResource struct {
	common.ResourceBase
}

type subdomainModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Subdomain      types.String `tfsdk:"subdomain"`
	DocumentRoot   types.String `tfsdk:"document_root"`
	Domain         types.String `tfsdk:"domain"`
}

// fill copies a listed subdomain. The list reports the default document root of every
// subdomain, so a document root already in the state is kept.
func (m *subdomainModel) fill(s *client.HostingSubdomain) {
	full := strings.ToLower(s.FullDomain)
	m.ID = types.StringValue(full)
	m.Domain = types.StringValue(strings.ToLower(s.Domain))
	m.Subdomain = types.StringValue(strings.TrimSuffix(full, "."+strings.ToLower(s.Domain)))
	if m.DocumentRoot.IsNull() || m.DocumentRoot.IsUnknown() {
		m.DocumentRoot = types.StringValue(s.DocumentRoot)
	}
}

// findSubdomain finds a subdomain by its full domain, or by its label on import.
func findSubdomain(subdomains []client.HostingSubdomain, key string) *client.HostingSubdomain {
	key = strings.ToLower(key)
	for i := range subdomains {
		full := strings.ToLower(subdomains[i].FullDomain)
		if full == key || full == key+"."+strings.ToLower(subdomains[i].Domain) {
			return &subdomains[i]
		}
	}
	return nil
}

func (r *subdomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_subdomain"
}

func (r *subdomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a subdomain of the main domain of a web hosting.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The full domain name of the subdomain, such as `blog.example.com`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			attrHostingID: hostingIDAttribute("subdomain"),
			"subdomain": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The subdomain, without the main domain: `blog` for `blog.example.com`. Lowercase " +
					"letters, digits, hyphens and dots, 63 characters at most. Changing it recreates the subdomain.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(63),
					stringvalidator.RegexMatches(subdomainPattern, "must be lowercase labels of letters, digits and hyphens, such as blog"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"document_root": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The directory the subdomain serves, relative to the account's home directory, such " +
					"as `public_html/blog`. When unset, the web hosting's default directory for the subdomain. The web " +
					"hosting does not report it, so a change made outside Terraform is not detected, and after an import " +
					"the configured value is taken as it is. Changing it recreates the subdomain.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1), common.NoSurroundingWhitespace()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					documentRootReplace{},
				},
			},
			"domain": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The main domain of the web hosting.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
	}
}

func (r *subdomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan subdomainModel
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
	hostingID, label := plan.HostingID.ValueString(), plan.Subdomain.ValueString()

	existing, err := c.ListHostingSubdomains(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the subdomains of the web hosting", err)
		return
	}
	if found := findSubdomain(existing, label); found != nil {
		resp.Diagnostics.AddAttributeError(path.Root("subdomain"), "Subdomain already exists",
			fmt.Sprintf("The web hosting already has %s. Import it rather than creating it again.", found.FullDomain))
		return
	}
	created, err := c.CreateHostingSubdomain(ctx, scope, hostingID, client.HostingSubdomainInput{
		Subdomain: label, DocumentRoot: plan.DocumentRoot.ValueString(),
	})
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the subdomain", err, map[string]path.Path{
			"subdomain": path.Root("subdomain"), "document_root": path.Root("document_root"),
		})
		return
	}
	listed, err := c.ListHostingSubdomains(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the subdomains of the web hosting", err)
		return
	}
	if found := findSubdomain(listed, created.FullDomain); found != nil {
		created = found
	}
	plan.fill(created)
	plan.Subdomain = types.StringValue(label)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *subdomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state subdomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	subdomains, gone, err := webChildList(ctx, c, webScope(state.OrganizationID, state.ProjectID), state.HostingID.ValueString(), c.ListHostingSubdomains)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the subdomains of the web hosting", err)
		return
	}
	found := findSubdomain(subdomains, state.ID.ValueString())
	if gone || found == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(found)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only runs after an import, to store the configured document root (documentRootReplace):
// every other change forces a replacement.
func (r *subdomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan subdomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, subdomainImportedKey, nil)...)
}

func (r *subdomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state subdomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID := webScope(state.OrganizationID, state.ProjectID), state.HostingID.ValueString()
	subdomains, gone, err := webChildList(ctx, c, scope, hostingID, c.ListHostingSubdomains)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the subdomains of the web hosting", err)
		return
	}
	found := findSubdomain(subdomains, state.ID.ValueString())
	if gone || found == nil {
		return
	}
	// The full domain names the subdomain unambiguously, whatever its number of labels.
	if err := c.DeleteHostingSubdomain(ctx, scope, hostingID, strings.ToLower(found.FullDomain)); err != nil {
		webChildDeleteFailed(ctx, c, scope, hostingID, err, &resp.Diagnostics, "Could not delete the subdomain")
	}
}

func (r *subdomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importWebChild(ctx, &r.ResourceBase, req, resp, "subdomain")
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, subdomainImportedKey, []byte("true"))...)
}

// subdomainImportedKey marks, in the private state, a subdomain imported and not applied since:
// its document root in the state is the default one, whatever the subdomain really serves.
const subdomainImportedKey = "imported_document_root"

// documentRootReplace forces a replacement when the configured document root changes, except
// after an import: the web hosting cannot tell the real one, so the configured value is stored
// as it is.
type documentRootReplace struct{}

func (documentRootReplace) Description(context.Context) string {
	return "Changing it recreates the subdomain, except right after an import."
}

func (m documentRootReplace) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (documentRootReplace) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || req.ConfigValue.IsNull() || req.PlanValue.IsUnknown() ||
		req.PlanValue.Equal(req.StateValue) {
		return
	}
	imported, diags := req.Private.GetKey(ctx, subdomainImportedKey)
	resp.Diagnostics.Append(diags...)
	if string(imported) == "true" {
		return
	}
	resp.RequiresReplace = true
}
