package backup

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

const powerActionTimeout = 10 * time.Minute

var _ action.ActionWithConfigure = (*powerAction)(nil)

// powerAction is onetsolutions_instance_reboot or onetsolutions_instance_reset: one request,
// one task, no other input than the instance.
type powerAction struct {
	actionBase
	name        string
	what        string
	description string
	start       func(*client.Client, context.Context, client.Scope, string) (string, error)
}

// NewInstanceRebootAction returns the onetsolutions_instance_reboot action.
func NewInstanceRebootAction() action.Action {
	return &powerAction{
		name:        "_instance_reboot",
		what:        "reboot",
		description: "Reboots an instance gracefully: its system receives a restart signal and shuts down cleanly first.",
		start:       (*client.Client).RebootInstance,
	}
}

// NewInstanceResetAction returns the onetsolutions_instance_reset action.
func NewInstanceResetAction() action.Action {
	return &powerAction{
		name:        "_instance_reset",
		what:        "reset",
		description: "Resets an instance at once, like a power cycle, without a clean shutdown of its system.",
		start:       (*client.Client).ResetInstance,
	}
}

func (a *powerAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + a.name
}

func (a *powerAction) Schema(ctx context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: a.description,
		Attributes:          targetAttributes(nil),
		Blocks:              timeoutsBlock(ctx, powerActionTimeout),
	}
}

func (a *powerAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var target instanceTarget
	resp.Diagnostics.Append(req.Config.Get(ctx, &target)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a.run(ctx, target, powerActionTimeout, a.what, resp, func(ctx context.Context, c *client.Client, scope client.Scope) (string, error) {
		return a.start(c, ctx, scope, target.InstanceID.ValueString())
	})
}
