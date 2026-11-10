package base

import (
	"context"
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
	_ resource.ResourceWithConfigure   = (*sshKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sshKeyResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*sshKeyResource)(nil)
)

// NewSSHKeyResource returns the onetsolutions_ssh_key resource.
func NewSSHKeyResource() resource.Resource {
	return &sshKeyResource{}
}

type sshKeyResource struct {
	common.ResourceBase
}

type sshKeyModel struct {
	ID             types.String         `tfsdk:"id"`
	OrganizationID types.String         `tfsdk:"organization_id"`
	ProjectID      types.String         `tfsdk:"project_id"`
	Name           types.String         `tfsdk:"name"`
	PublicKey      common.TrimmedString `tfsdk:"public_key"`
	Fingerprint    types.String         `tfsdk:"fingerprint"`
	Created        types.String         `tfsdk:"created"`
}

func (m *sshKeyModel) fill(key *client.SSHKey, scope client.Scope) {
	m.ID = types.StringValue(key.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.Name = types.StringValue(key.Name)
	m.PublicKey = common.NewTrimmedString(key.PublicKey)
	m.Fingerprint = types.StringValue(key.Fingerprint)
	m.Created = types.StringValue(key.Created)
}

func (m *sshKeyModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an SSH public key of a project, to install on new instances.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("SSH key"),
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The name of the key, unique in the project.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255), common.NoSurroundingWhitespace()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public_key": schema.StringAttribute{
				CustomType: common.TrimmedStringType{},
				Required:   true,
				MarkdownDescription: "The public key in OpenSSH format (`ssh-ed25519 AAAA… comment`). Surrounding " +
					"whitespace, such as the trailing newline `file()` reads, is ignored. After an import the state " +
					"holds the key trimmed: a configuration with a trailing newline then plans one in-place update " +
					"that changes nothing.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(publicKeyPattern, "must be an OpenSSH public key of type ssh-ed25519, ssh-rsa, ssh-dss or ecdsa-sha2-*"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(publicKeyChanged,
						"Replacing the key unless only surrounding whitespace changed.",
						"Replacing the key unless only surrounding whitespace changed."),
				},
			},
			"fingerprint": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The SHA256 fingerprint of the key, as `ssh-keygen -l` prints it.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the key was registered (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
	}
}

func publicKeyChanged(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = strings.TrimSpace(req.PlanValue.ValueString()) != strings.TrimSpace(req.StateValue.ValueString())
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshKeyModel
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

	key, err := c.CreateSSHKey(ctx, scope, client.SSHKeyCreate{
		Name:      plan.Name.ValueString(),
		PublicKey: plan.PublicKey.ValueString(),
	})
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the SSH key", err, map[string]path.Path{
			"name": path.Root("name"), "public_key": path.Root("public_key"),
		})
		return
	}
	plan.fill(key, scope)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := c.GetSSHKey(ctx, state.scope(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the SSH key", err)
		return
	}
	state.fill(key, state.scope())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only runs when public_key changed by surrounding whitespace: nothing to send.
func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	err := c.DeleteSSHKey(ctx, state.scope(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		common.AddClientError(&resp.Diagnostics, "Could not delete the SSH key", err)
	}
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
}
