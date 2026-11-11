package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/action/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
)

// actionBase is embedded by every action: it receives the provider data and runs the task an
// action queues on an instance.
type actionBase struct {
	data *common.ProviderData
}

func (b *actionBase) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*common.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("Expected *common.ProviderData, got %T. This is a bug in the provider; please report it.", req.ProviderData))
		return
	}
	b.data = data
}

// instanceTarget is what every action's configuration holds: the instance, where it lives, and
// how long to wait.
type instanceTarget struct {
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	InstanceID     types.String   `tfsdk:"instance_id"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

// targetAttributes returns organization_id, project_id and instance_id, merged with extra.
func targetAttributes(extra map[string]schema.Attribute) map[string]schema.Attribute {
	return common.MergeAttributes(map[string]schema.Attribute{
		common.AttrOrganizationID: schema.StringAttribute{
			Optional:            true,
			MarkdownDescription: "The organization of the instance. Defaults to the provider's `organization_id`.",
			Validators:          []validator.String{common.UUID()},
		},
		common.AttrProjectID: schema.StringAttribute{
			Optional:            true,
			MarkdownDescription: "The project of the instance. Defaults to the provider's `project_id`.",
			Validators:          []validator.String{common.UUID()},
		},
		"instance_id": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "The UUID of the instance.",
			Validators:          []validator.String{common.UUID()},
		},
	}, extra)
}

func timeoutsBlock(ctx context.Context, defaultTimeout time.Duration) map[string]schema.Block {
	return map[string]schema.Block{
		common.AttrTimeouts: timeouts.BlockWithOpts(ctx, timeouts.Opts{
			InvokeDescription: fmt.Sprintf("How long to wait for the action to complete, as a duration such as "+
				"\"30s\" or \"2h45m\". Defaults to `%s`.", formatDuration(defaultTimeout)),
		}),
	}
}

func formatDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", d/time.Minute)
}

// run starts an operation on the instance, retrying while another task holds it, then waits for
// the task it queued, reporting each step. what names the operation in messages ("reboot").
func (b *actionBase) run(ctx context.Context, target instanceTarget, defaultTimeout time.Duration, what string,
	resp *action.InvokeResponse, start func(context.Context, *client.Client, client.Scope) (string, error)) {
	c := b.data.RequireClient(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, diags := b.data.ResolveScope(ctx, target.OrganizationID, target.ProjectID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := target.Timeouts.Invoke(ctx, defaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	instanceID := target.InstanceID.ValueString()
	progress := func(message string) {
		if resp.SendProgress != nil {
			resp.SendProgress(action.InvokeProgressEvent{Message: message})
		}
	}
	var taskID string
	err := retryWhileBusy(ctx, b.data.Waiter, what+" of instance "+instanceID, func(ctx context.Context) error {
		var err error
		taskID, err = start(ctx, c, scope)
		if instanceBusy(err) {
			progress(fmt.Sprintf("Instance %s is busy with another operation: waiting to start the %s.", instanceID, what))
		}
		return err
	})
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not start the "+what, err)
		return
	}
	progress(fmt.Sprintf("The %s of instance %s started (task %s).", what, instanceID, taskID))
	err = waitTask(ctx, b.data.Waiter, c, scope, instanceID, taskID, func(task *client.InstanceTask) {
		progress(fmt.Sprintf("The %s of instance %s is %s (%d%%).", what, instanceID, task.Status, task.Progress))
	})
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "The "+what+" did not complete", err)
	}
}
