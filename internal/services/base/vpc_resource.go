package base

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure   = (*vpcResource)(nil)
	_ resource.ResourceWithImportState = (*vpcResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*vpcResource)(nil)
)

var vpcTimeouts = common.Timeouts{Delete: 10 * time.Minute}

// NewVPCResource returns the onetsolutions_vpc resource.
func NewVPCResource() resource.Resource {
	return &vpcResource{}
}

type vpcResource struct {
	common.ResourceBase
}

type vpcModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	Name           types.String   `tfsdk:"name"`
	CIDRBlock      types.String   `tfsdk:"cidr_block"`
	Created        types.String   `tfsdk:"created"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (m *vpcModel) fill(vpc *client.VPC, scope client.Scope) {
	m.ID = types.StringValue(vpc.ID)
	m.OrganizationID = types.StringValue(scope.OrganizationID)
	m.ProjectID = types.StringValue(scope.ProjectID)
	m.Name = types.StringValue(vpc.Name)
	m.CIDRBlock = types.StringValue(vpc.CIDRBlock)
	m.Created = types.StringValue(vpc.Created)
}

func (m *vpcModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (r *vpcResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

func (r *vpcResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a VPC, a private IPv4 network that instances join through their `vpc_id`.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: common.IDAttribute("VPC"),
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The name of the VPC, unique in the project. Can be changed in place.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255), common.NoSurroundingWhitespace()},
			},
			"cidr_block": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The private IPv4 range of the VPC in CIDR notation, such as `10.0.0.0/16`, inside " +
					"10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16. Changing it recreates the VPC.",
				Validators:    []validator.String{common.IPv4CIDR()},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the VPC was created (RFC 3339).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		}),
		Blocks: map[string]schema.Block{
			common.AttrTimeouts: common.TimeoutsBlock(ctx, vpcTimeouts),
		},
	}
}

func (r *vpcResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpcModel
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

	vpc, err := c.CreateVPC(ctx, scope, client.VPCCreate{
		Name:      plan.Name.ValueString(),
		CIDRBlock: plan.CIDRBlock.ValueString(),
	})
	if err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the VPC", err, map[string]path.Path{
			"name": path.Root("name"), "cidr_block": path.Root("cidr_block"),
		})
		return
	}
	plan.fill(vpc, scope)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vpcResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpcModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	vpc, err := c.GetVPC(ctx, state.scope(), state.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the VPC", err)
		return
	}
	state.fill(vpc, state.scope())
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *vpcResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vpcModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		vpc, err := c.UpdateVPC(ctx, state.scope(), state.ID.ValueString(), client.VPCUpdate{Name: &name})
		if err != nil {
			common.AddClientErrorWithFields(&resp.Diagnostics, "Could not rename the VPC", err, map[string]path.Path{
				"name": path.Root("name"),
			})
			return
		}
		plan.fill(vpc, state.scope())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *vpcResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpcModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, vpcTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := waiter.Retry(ctx, r.Data().Waiter, "deleting VPC "+state.ID.ValueString(),
		func(ctx context.Context) error {
			err := c.DeleteVPC(ctx, state.scope(), state.ID.ValueString())
			if client.IsNotFound(err) {
				return nil
			}
			return err
		},
		vpcStillInUse)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not delete the VPC", err)
	}
}

// vpcStillInUse matches the API's refusal while instances are attached. The API answers it with
// HTTP 400 and the code vpc_in_use; a 409 is retried too.
func vpcStillInUse(err error) bool {
	return client.HasCode(err, client.CodeVPCInUse) || client.IsConflict(err)
}

func (r *vpcResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
}
