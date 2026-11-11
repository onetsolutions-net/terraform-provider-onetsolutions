package hosting

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

var (
	_ resource.ResourceWithConfigure   = (*redirectResource)(nil)
	_ resource.ResourceWithImportState = (*redirectResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*redirectResource)(nil)
)

// RedirectRemovalWarning is the plan-time warning of a redirect being destroyed or replaced.
const RedirectRemovalWarning = "Terraform cannot remove a redirect: destroying this resource only removes it " +
	"from the Terraform state, and the redirect keeps working until it is removed in the web hosting's control panel. " +
	"A replacement needs that removal first when it keeps the same domain and source."

var redirectSourcePattern = regexp.MustCompile(`^/\S*$`)

// redirectTypes maps the spellings of a redirect type to its status code.
var redirectTypes = map[string]string{
	"301": "301", "permanent": "301",
	"302": "302", "temp": "302", "temporary": "302",
}

// NewRedirectResource returns the onetsolutions_hosting_redirect resource.
func NewRedirectResource() resource.Resource {
	return &redirectResource{}
}

type redirectResource struct {
	common.ResourceBase
}

type redirectModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Domain         types.String `tfsdk:"domain"`
	Source         types.String `tfsdk:"source"`
	Destination    types.String `tfsdk:"destination"`
	Type           types.String `tfsdk:"type"`
}

func redirectSource(source string) string {
	return "/" + strings.TrimPrefix(source, "/")
}

func redirectID(domain, source string) string {
	return strings.ToLower(domain) + redirectSource(source)
}

// splitRedirectID splits "example.com/old" into its domain and source.
func splitRedirectID(id string) (domain, source string) {
	domain, rest, _ := strings.Cut(id, "/")
	return domain, "/" + rest
}

func redirectType(t string) string {
	if code, ok := redirectTypes[strings.ToLower(t)]; ok {
		return code
	}
	return t
}

// fill copies a listed redirect, keeping the spelling of the state for the same destination and
// type.
func (m *redirectModel) fill(r *client.HostingRedirect) {
	m.ID = types.StringValue(redirectID(r.Domain, r.Source))
	m.Domain = types.StringValue(strings.ToLower(r.Domain))
	m.Source = types.StringValue(redirectSource(r.Source))
	if m.Destination.IsNull() || strings.TrimSuffix(m.Destination.ValueString(), "/") != strings.TrimSuffix(r.Destination, "/") {
		m.Destination = types.StringValue(r.Destination)
	}
	if m.Type.IsNull() || redirectType(m.Type.ValueString()) != redirectType(r.Type) {
		m.Type = types.StringValue(redirectType(r.Type))
	}
}

// findRedirect finds the redirect of a domain and source, preferring the one to destination
// when several match.
func findRedirect(redirects []client.HostingRedirect, domain, source, destination string) *client.HostingRedirect {
	var found *client.HostingRedirect
	for i := range redirects {
		r := &redirects[i]
		if !strings.EqualFold(r.Domain, domain) || redirectSource(r.Source) != redirectSource(source) {
			continue
		}
		if r.Destination == destination {
			return r
		}
		if found == nil {
			found = r
		}
	}
	return found
}

func (r *redirectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_redirect"
}

func (r *redirectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a redirect of a web hosting, from a path of one of its domains to another URL.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The domain followed by the source path, such as `example.com/old-page`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			attrHostingID: hostingIDAttribute("redirect"),
			"domain": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The domain of the web hosting the redirect applies to: its main domain, a subdomain " +
					"or an addon domain, lowercase. Changing it adds another redirect and leaves the previous one in place.",
				Validators:    webDomainValidators(),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"source": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The path to redirect, starting with `/`, such as `/old-page`; `/` redirects the " +
					"whole domain. Changing it adds another redirect and leaves the previous one in place.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(redirectSourcePattern, "must be a path starting with /, without spaces"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"destination": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The `http` or `https` URL to redirect to. A change fails while the previous redirect of " +
					"this `source` is in place: remove it in the web hosting's control panel first (see the page's notes).",
				Validators:    []validator.String{httpURL{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("301"),
				MarkdownDescription: "`301` (permanent, the default) or `302` (temporary). A change fails while the previous " +
					"redirect of this `source` is in place: remove it in the web hosting's control panel first (see the page's notes).",
				Validators:    []validator.String{stringvalidator.OneOf("301", "302")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		}),
	}
}

// ModifyPlan warns that destroying or replacing a redirect leaves it on the web hosting, and
// refuses a new destination or type for the same domain and source: the replacement would fail
// on the redirect still in place, after removing the resource from the state.
func (r *redirectResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	r.PlanScope(ctx, req, resp)
	if req.State.Raw.IsNull() || resp.Diagnostics.HasError() {
		return
	}
	if req.Plan.Raw.IsNull() {
		resp.Diagnostics.AddWarning("The redirect stays on the web hosting", RedirectRemovalWarning)
		return
	}
	var plan, state redirectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if attribute, changed := changedInPlace(&plan, &state); changed {
		resp.Diagnostics.AddAttributeError(path.Root(attribute), "A redirect cannot be changed in place",
			fmt.Sprintf("%s%s redirects to %s (%s), and Terraform cannot remove a redirect. Remove it in the web "+
				"hosting's control panel first: the next plan then finds it gone and adds the new one.",
				state.Domain.ValueString(), state.Source.ValueString(), state.Destination.ValueString(), state.Type.ValueString()))
		return
	}
	// Every argument forces a replacement, and resp.RequiresReplace is not filled yet here.
	if !plan.HostingID.Equal(state.HostingID) || !plan.Domain.Equal(state.Domain) || !plan.Source.Equal(state.Source) ||
		!plan.Destination.Equal(state.Destination) || !plan.Type.Equal(state.Type) {
		resp.Diagnostics.AddWarning("The redirect stays on the web hosting", RedirectRemovalWarning)
	}
}

// changedInPlace reports whether the plan keeps the web hosting, domain and source of the state
// but changes the destination or the type, and names the attribute that changed.
func changedInPlace(plan, state *redirectModel) (string, bool) {
	if !common.Known(plan.HostingID, plan.Domain, plan.Source, plan.Destination, plan.Type) ||
		!plan.HostingID.Equal(state.HostingID) || !strings.EqualFold(plan.Domain.ValueString(), state.Domain.ValueString()) ||
		redirectSource(plan.Source.ValueString()) != redirectSource(state.Source.ValueString()) {
		return "", false
	}
	if strings.TrimSuffix(plan.Destination.ValueString(), "/") != strings.TrimSuffix(state.Destination.ValueString(), "/") {
		return "destination", true
	}
	if redirectType(plan.Type.ValueString()) != redirectType(state.Type.ValueString()) {
		return "type", true
	}
	return "", false
}

func (r *redirectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan redirectModel
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
	hostingID := plan.HostingID.ValueString()
	in := client.HostingRedirect{
		Domain: plan.Domain.ValueString(), Source: plan.Source.ValueString(),
		Destination: plan.Destination.ValueString(), Type: plan.Type.ValueString(),
	}

	existing, err := c.ListHostingRedirects(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the redirects of the web hosting", err)
		return
	}
	if found := findRedirect(existing, in.Domain, in.Source, in.Destination); found != nil {
		if found.Destination != in.Destination || redirectType(found.Type) != in.Type {
			resp.Diagnostics.AddError("Redirect already exists",
				fmt.Sprintf("%s%s already redirects to %s (%s). Terraform cannot remove a redirect: remove it in "+
					"the web hosting's control panel, then apply again.", in.Domain, in.Source, found.Destination, redirectType(found.Type)))
			return
		}
		// The same redirect is already in place, typically left by an earlier destroy: it is
		// taken over rather than added a second time.
		plan.fill(found)
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		return
	}
	created, err := c.CreateHostingRedirect(ctx, scope, hostingID, in)
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the redirect", err, map[string]path.Path{
			"domain": path.Root("domain"), "source": path.Root("source"),
			"destination": path.Root("destination"), "type": path.Root("type"),
		})
		return
	}
	listed, err := c.ListHostingRedirects(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the redirects of the web hosting", err)
		return
	}
	if found := findRedirect(listed, in.Domain, in.Source, in.Destination); found != nil {
		created = found
	}
	plan.fill(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *redirectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state redirectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	redirects, gone, err := webChildList(ctx, c, webScope(state.OrganizationID, state.ProjectID), state.HostingID.ValueString(), c.ListHostingRedirects)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the redirects of the web hosting", err)
		return
	}
	domain, source := splitRedirectID(state.ID.ValueString())
	found := findRedirect(redirects, domain, source, state.Destination.ValueString())
	if gone || found == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(found)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called: every argument forces a replacement.
func (r *redirectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan redirectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete only removes the redirect from the state: Terraform cannot remove one.
func (r *redirectResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("The redirect stays on the web hosting", RedirectRemovalWarning)
}

func (r *redirectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importWebChild(ctx, &r.ResourceBase, req, resp, "domain>/<path")
}

// httpURL validates an absolute http or https URL.
type httpURL struct{}

func (httpURL) Description(context.Context) string {
	return "value must be an http or https URL, such as https://example.com/new-page"
}

func (v httpURL) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (httpURL) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.ContainsAny(value, " \t\n") {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid URL",
			fmt.Sprintf("%q is not an http or https URL, such as https://example.com/new-page.", value))
	}
}
