package base

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

// publicKeyPattern mirrors the API's check: a supported key type, one space, base64.
var publicKeyPattern = regexp.MustCompile(`(?s)^\s*(ssh-ed25519|ssh-rsa|ssh-dss|ecdsa-sha2-[a-z0-9-]+) [A-Za-z0-9+/]+={0,3}(\s.*)?$`)

var (
	_ datasource.DataSourceWithConfigure        = (*sshKeyDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*sshKeyDataSource)(nil)
)

// NewSSHKeyDataSource returns the onetsolutions_ssh_key data source.
func NewSSHKeyDataSource() datasource.DataSource {
	return &sshKeyDataSource{}
}

type sshKeyDataSource struct {
	common.DataSourceBase
}

type sshKeyDataModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	Name           types.String `tfsdk:"name"`
	PublicKey      types.String `tfsdk:"public_key"`
	Fingerprint    types.String `tfsdk:"fingerprint"`
	Created        types.String `tfsdk:"created"`
}

func (d *sshKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (d *sshKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an SSH key of a project by `id` or by `name`.",
		Attributes: common.MergeAttributes(common.DataSourceScopeAttributes(), map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The UUID of the key. Exactly one of `id` and `name` is required.",
				Validators:          []validator.String{common.UUID()},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The name of the key. Exactly one of `id` and `name` is required.",
			},
			"public_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The public key in OpenSSH format.",
			},
			"fingerprint": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The SHA256 fingerprint of the key.",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the key was registered (RFC 3339).",
			},
		}),
	}
}

func (d *sshKeyDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *sshKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config sshKeyDataModel
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

	var key *client.SSHKey
	if !config.ID.IsNull() {
		found, err := c.GetSSHKey(ctx, scope, config.ID.ValueString())
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not read the SSH key", err)
			return
		}
		key = found
	} else {
		keys, err := c.ListSSHKeys(ctx, scope)
		if err != nil {
			common.AddClientError(&resp.Diagnostics, "Could not list the SSH keys", err)
			return
		}
		name := config.Name.ValueString()
		found, diags := common.FindOne(keys,
			func(k client.SSHKey) bool { return k.Name == name },
			func(k client.SSHKey) string { return k.ID },
			"SSH key", fmt.Sprintf("name %q", name))
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		key = &found
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, sshKeyDataModel{
		ID:             types.StringValue(key.ID),
		OrganizationID: types.StringValue(scope.OrganizationID),
		ProjectID:      types.StringValue(scope.ProjectID),
		Name:           types.StringValue(key.Name),
		PublicKey:      types.StringValue(key.PublicKey),
		Fingerprint:    types.StringValue(key.Fingerprint),
		Created:        types.StringValue(key.Created),
	})...)
}
