package network

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/services/common"
	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/waiter"
)

var (
	_ resource.ResourceWithConfigure      = (*instanceFirewallResource)(nil)
	_ resource.ResourceWithImportState    = (*instanceFirewallResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*instanceFirewallResource)(nil)
	_ resource.ResourceWithValidateConfig = (*instanceFirewallResource)(nil)
)

var firewallTimeouts = common.Timeouts{Create: 10 * time.Minute, Update: 10 * time.Minute, Delete: 10 * time.Minute}

const (
	defaultSourceCIDR = "0.0.0.0/0"
	minRulePriority   = 1
	maxRulePriority   = 10000
)

// NewInstanceFirewallResource returns the onetsolutions_instance_firewall resource.
func NewInstanceFirewallResource() resource.Resource {
	return &instanceFirewallResource{}
}

type instanceFirewallResource struct {
	common.ResourceBase
}

type instanceFirewallModel struct {
	ID             types.String   `tfsdk:"id"`
	OrganizationID types.String   `tfsdk:"organization_id"`
	ProjectID      types.String   `tfsdk:"project_id"`
	InstanceID     types.String   `tfsdk:"instance_id"`
	Rules          types.List     `tfsdk:"rule"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

type firewallRuleModel struct {
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Direction       types.String `tfsdk:"direction"`
	Action          types.String `tfsdk:"action"`
	Protocol        types.String `tfsdk:"protocol"`
	SourceCIDR      types.String `tfsdk:"source_cidr"`
	DestinationCIDR types.String `tfsdk:"destination_cidr"`
	DestinationPort types.String `tfsdk:"destination_port"`
	Priority        types.Int64  `tfsdk:"priority"`
	Enabled         types.Bool   `tfsdk:"enabled"`
}

var firewallRuleType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"name":             types.StringType,
	"description":      types.StringType,
	"direction":        types.StringType,
	"action":           types.StringType,
	"protocol":         types.StringType,
	"source_cidr":      types.StringType,
	"destination_cidr": types.StringType,
	"destination_port": types.StringType,
	"priority":         types.Int64Type,
	"enabled":          types.BoolType,
}}

func (m *instanceFirewallModel) scope() client.Scope {
	return client.Scope{OrganizationID: m.OrganizationID.ValueString(), ProjectID: m.ProjectID.ValueString()}
}

func (m *instanceFirewallModel) rules(ctx context.Context) ([]firewallRuleModel, diag.Diagnostics) {
	var rules []firewallRuleModel
	if m.Rules.IsNull() || m.Rules.IsUnknown() {
		return rules, nil
	}
	diags := m.Rules.ElementsAs(ctx, &rules, false)
	return rules, diags
}

func ruleFromAPI(rule *client.FirewallRule) firewallRuleModel {
	return firewallRuleModel{
		Name:            types.StringValue(rule.Name),
		Description:     types.StringValue(rule.Description),
		Direction:       types.StringValue(rule.Direction),
		Action:          types.StringValue(rule.Action),
		Protocol:        types.StringValue(rule.Protocol),
		SourceCIDR:      types.StringValue(rule.SourceCIDR),
		DestinationCIDR: emptyAsNull(rule.DestinationCIDR),
		DestinationPort: emptyAsNull(rule.DestinationPort),
		Priority:        types.Int64Value(rule.Priority),
		Enabled:         types.BoolValue(rule.IsEnabled),
	}
}

// emptyAsNull maps the API's "" (no destination, every port) to an unset attribute.
func emptyAsNull(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func (r *instanceFirewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_firewall"
}

func (r *instanceFirewallResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the customer firewall rules of an instance.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The UUID of the instance: an instance has one firewall.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"instance_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The UUID of the instance. Changing it moves the rules to another instance (replacement).",
				Validators:          []validator.String{common.UUID()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		}),
		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{
				MarkdownDescription: "A customer rule. Omit every `rule` block to keep no customer rule at all. After an " +
					"import the rules are read in priority order: list them in that order to avoid a reorder-only change.",
				NestedObject: schema.NestedBlockObject{Attributes: firewallRuleAttributes()},
			},
			common.AttrTimeouts: common.TimeoutsBlock(ctx, firewallTimeouts),
		},
	}
}

func firewallRuleAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Required: true,
			MarkdownDescription: "The name of the rule, unique among the instance's rules: rules are matched by " +
				"name. It must not start with `One Click Apps · ` or `one-click:`, which name platform rules.",
			Validators: []validator.String{stringvalidator.LengthBetween(1, 255), common.NoSurroundingWhitespace(), unmanagedName{}},
		},
		"description": schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "A free-form description of the rule. Defaults to an empty string.",
			Default:             stringdefault.StaticString(""),
			Validators:          []validator.String{common.NoSurroundingWhitespace()},
		},
		"direction": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "The traffic the rule applies to: `inbound` or `outbound`.",
			Validators:          []validator.String{stringvalidator.OneOf("inbound", "outbound")},
		},
		"action": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "What the rule does with matching traffic: `accept`, `drop` or `reject`.",
			Validators:          []validator.String{stringvalidator.OneOf("accept", "drop", "reject")},
		},
		"protocol": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "The protocol matched: `tcp`, `udp`, `icmp` or `all`.",
			Validators:          []validator.String{stringvalidator.OneOf("tcp", "udp", "icmp", "all")},
		},
		"source_cidr": schema.StringAttribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "The source network in CIDR notation, such as `203.0.113.0/24`. Defaults to " +
				"`0.0.0.0/0` (any source). A rule whose source is a platform address becomes platform-managed and is " +
				"refused.",
			Default:    stringdefault.StaticString(defaultSourceCIDR),
			Validators: []validator.String{common.CIDR()},
		},
		"destination_cidr": schema.StringAttribute{
			Optional:            true,
			MarkdownDescription: "The destination network in CIDR notation. When unset, matches any destination.",
			Validators:          []validator.String{common.CIDR()},
		},
		"destination_port": schema.StringAttribute{
			Optional: true,
			MarkdownDescription: "The destination ports: a port (`443`), a range (`1000-2000`) or a comma-separated " +
				"list of both. Only for `tcp` and `udp`; unset matches every port.",
			Validators: []validator.String{common.PortSpec()},
		},
		"priority": schema.Int64Attribute{
			Required:            true,
			MarkdownDescription: "The order in which rules are evaluated, from 1 to 10000: the lowest first.",
			Validators:          []validator.Int64{int64validator.Between(minRulePriority, maxRulePriority)},
		},
		"enabled": schema.BoolAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Whether the rule is in force. Defaults to `true`.",
			Default:             booldefault.StaticBool(true),
		},
	}
}

func (r *instanceFirewallResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var rules types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("rule"), &rules)...)
	if resp.Diagnostics.HasError() || rules.IsNull() || rules.IsUnknown() {
		return
	}
	var models []firewallRuleModel
	resp.Diagnostics.Append(rules.ElementsAs(ctx, &models, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	seen := map[string]int{}
	for i := range models {
		rule := &models[i]
		rulePath := path.Root("rule").AtListIndex(i)
		if !rule.Name.IsNull() && !rule.Name.IsUnknown() {
			name := rule.Name.ValueString()
			if first, duplicate := seen[name]; duplicate {
				resp.Diagnostics.AddAttributeError(rulePath.AtName("name"), "Duplicate rule name",
					fmt.Sprintf("Rules %d and %d are both named %q. The provider matches rules by name, so names must be unique.",
						first, i, name))
			} else {
				seen[name] = i
			}
		}
		protocolKnown := !rule.Protocol.IsNull() && !rule.Protocol.IsUnknown()
		if protocolKnown && !rule.DestinationPort.IsNull() && !rule.DestinationPort.IsUnknown() {
			if protocol := rule.Protocol.ValueString(); protocol != "tcp" && protocol != "udp" {
				resp.Diagnostics.AddAttributeError(rulePath.AtName("destination_port"), "Port without TCP or UDP",
					fmt.Sprintf("Ports only apply to tcp and udp; leave destination_port unset for protocol %q.", protocol))
			}
		}
	}
}

func (r *instanceFirewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceFirewallModel
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
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpCreate, firewallTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.OrganizationID = types.StringValue(scope.OrganizationID)
	plan.ProjectID = types.StringValue(scope.ProjectID)
	resp.Diagnostics.Append(r.converge(ctx, c, &plan, "Could not create the firewall rules")...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.InstanceID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *instanceFirewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceFirewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.InstanceID.IsNull() {
		state.InstanceID = state.ID
	}
	gone, err := instanceGone(ctx, c, state.scope(), state.InstanceID.ValueString())
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the instance of the firewall", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	rules, err := c.ListFirewallRules(ctx, state.scope(), state.InstanceID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the firewall rules", err)
		return
	}
	prior, diags := state.rules(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	list, diags := types.ListValueFrom(ctx, firewallRuleType, orderLikeState(prior, customerRules(rules)))
	resp.Diagnostics.Append(diags...)
	state.Rules = list
	state.ID = state.InstanceID
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// orderLikeState returns the customer rules as state models, in the order of the rules already in
// state (matched by name), then the rules the state does not know in the API's priority order:
// a rule added outside Terraform shows as one to remove, a missing one as one to recreate.
func orderLikeState(prior []firewallRuleModel, rules []client.FirewallRule) []firewallRuleModel {
	used := make([]bool, len(rules))
	out := make([]firewallRuleModel, 0, len(rules))
	for k := range prior {
		for i := range rules {
			if !used[i] && rules[i].Name == prior[k].Name.ValueString() {
				used[i] = true
				out = append(out, ruleFromAPI(&rules[i]))
				break
			}
		}
	}
	for i := range rules {
		if !used[i] {
			out = append(out, ruleFromAPI(&rules[i]))
		}
	}
	return out
}

func (r *instanceFirewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state instanceFirewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, plan.Timeouts, common.OpUpdate, firewallTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.OrganizationID, plan.ProjectID = state.OrganizationID, state.ProjectID
	resp.Diagnostics.Append(r.converge(ctx, c, &plan, "Could not update the firewall rules")...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = plan.InstanceID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *instanceFirewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceFirewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel, diags := common.WithTimeout(ctx, state.Timeouts, common.OpDelete, firewallTimeouts)
	defer cancel()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	scope, instanceID := state.scope(), state.InstanceID.ValueString()
	existing, err := c.ListFirewallRules(ctx, scope, instanceID)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the firewall rules", err)
		return
	}
	stale := customerRules(existing)
	if len(stale) == 0 {
		return
	}
	before, err := recentTaskIDs(ctx, c, scope, instanceID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not read the tasks of the instance", err)
		return
	}
	for i := range stale {
		if err := r.deleteRule(ctx, c, scope, instanceID, &stale[i]); err != nil {
			if goneWithInstance(ctx, c, scope, instanceID, err) {
				return
			}
			common.AddClientError(&resp.Diagnostics, "Could not delete the firewall rule "+stale[i].Name, err)
			return
		}
	}
	if err := r.applyChanges(ctx, c, scope, instanceID, before); err != nil && !goneWithInstance(ctx, c, scope, instanceID, err) {
		common.AddClientError(&resp.Diagnostics, "Could not apply the firewall", err)
	}
}

func (r *instanceFirewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	r.ImportScoped(ctx, req, resp)
}

// converge makes the customer rules of the instance equal to the plan's, then waits for the
// firewall to be applied with the last change. Rules are created and updated before stale ones
// are deleted, so a renamed rule never leaves a moment without it. Every change queues an
// apply_firewall of its own on the API side and is refused while the previous
// one runs, so changes are retried while the instance is busy; the provider sends no change it
// does not need (a new rule is patched only for what the create cannot set: destination_cidr and
// enabled = false) and no apply of its own after the last change, whose apply covers every rule.
func (r *instanceFirewallResource) converge(ctx context.Context, c *client.Client, plan *instanceFirewallModel, summary string) diag.Diagnostics {
	var diags diag.Diagnostics
	desired, d := plan.rules(ctx)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	scope, instanceID := plan.scope(), plan.InstanceID.ValueString()

	existing, err := c.ListFirewallRules(ctx, scope, instanceID)
	if err != nil {
		common.AddClientError(&diags, summary, err)
		return diags
	}
	before, err := recentTaskIDs(ctx, c, scope, instanceID)
	if err != nil {
		common.AddClientError(&diags, summary, err)
		return diags
	}
	byName := map[string][]client.FirewallRule{}
	customer := customerRules(existing)
	for i := range customer {
		byName[customer[i].Name] = append(byName[customer[i].Name], customer[i])
	}

	for i := range desired {
		want := &desired[i]
		name := want.Name.ValueString()
		if matches := byName[name]; len(matches) > 0 {
			byName[name] = matches[1:]
			if err := r.updateRule(ctx, c, scope, instanceID, &matches[0], want); err != nil {
				common.AddClientError(&diags, fmt.Sprintf("%s: rule %q", summary, name), err)
				return diags
			}
			continue
		}
		if err := r.createRule(ctx, c, scope, instanceID, want); err != nil {
			common.AddClientErrorWithFields(&diags, fmt.Sprintf("%s: rule %q", summary, name), err, rulePaths(i))
			return diags
		}
	}
	for _, stale := range byName {
		for i := range stale {
			if err := r.deleteRule(ctx, c, scope, instanceID, &stale[i]); err != nil {
				common.AddClientError(&diags, fmt.Sprintf("%s: deleting rule %q", summary, stale[i].Name), err)
				return diags
			}
		}
	}

	if err := r.applyChanges(ctx, c, scope, instanceID, before); err != nil {
		common.AddClientError(&diags, "Could not apply the firewall", err)
	}
	return diags
}

func rulePaths(index int) map[string]path.Path {
	rule := path.Root("rule").AtListIndex(index)
	paths := map[string]path.Path{"source": rule.AtName("source_cidr")}
	for _, name := range []string{"name", "description", "direction", "action", "protocol", "destination_port", "priority"} {
		paths[name] = rule.AtName(name)
	}
	return paths
}

func (r *instanceFirewallResource) createRule(ctx context.Context, c *client.Client, scope client.Scope, instanceID string, want *firewallRuleModel) error {
	var created *client.FirewallRule
	err := retryWhileBusy(ctx, r.Data().Waiter, "creating firewall rule "+want.Name.ValueString(), func(ctx context.Context) error {
		var err error
		created, err = c.CreateFirewallRule(ctx, scope, instanceID, client.FirewallRuleCreate{
			Name:            want.Name.ValueString(),
			Description:     want.Description.ValueString(),
			Direction:       want.Direction.ValueString(),
			Action:          want.Action.ValueString(),
			Protocol:        want.Protocol.ValueString(),
			Source:          want.SourceCIDR.ValueString(),
			DestinationPort: want.DestinationPort.ValueString(),
			Priority:        want.Priority.ValueInt64(),
		})
		return err
	})
	if err != nil {
		return err
	}
	if created.SourceLabel != nil {
		if err := r.deleteRule(ctx, c, scope, instanceID, created); err != nil {
			return err
		}
		return fmt.Errorf("the source %s is a platform address (%s): a rule from it is platform-managed and cannot be "+
			"managed by Terraform", created.SourceCIDR, *created.SourceLabel)
	}
	return r.updateRule(ctx, c, scope, instanceID, created, want)
}

// updateRule patches the fields of current that differ from want, if any.
func (r *instanceFirewallResource) updateRule(ctx context.Context, c *client.Client, scope client.Scope, instanceID string, current *client.FirewallRule, want *firewallRuleModel) error {
	body, changed := ruleChanges(current, want)
	if !changed {
		return nil
	}
	var updated *client.FirewallRule
	err := retryWhileBusy(ctx, r.Data().Waiter, "updating firewall rule "+current.Name, func(ctx context.Context) error {
		var err error
		updated, err = c.UpdateFirewallRule(ctx, scope, instanceID, current.ID, body)
		return err
	})
	if err != nil || updated.SourceLabel == nil {
		return err
	}
	// The new source is a platform address: the rule would turn platform-managed, leave the
	// state and never be deleted by Terraform again, so its source is put back.
	previous := current.SourceCIDR
	rollback := retryWhileBusy(ctx, r.Data().Waiter, "restoring the source of firewall rule "+current.Name, func(ctx context.Context) error {
		_, err := c.UpdateFirewallRule(ctx, scope, instanceID, current.ID, client.FirewallRuleUpdate{SourceCIDR: &previous})
		return err
	})
	if rollback != nil {
		return fmt.Errorf("the source %s is a platform address (%s), and restoring the rule's source %s failed: %w",
			updated.SourceCIDR, *updated.SourceLabel, previous, rollback)
	}
	return fmt.Errorf("the source %s is a platform address (%s): a rule from it is platform-managed and cannot be "+
		"managed by Terraform; the rule keeps its source %s", updated.SourceCIDR, *updated.SourceLabel, previous)
}

func ruleChanges(current *client.FirewallRule, want *firewallRuleModel) (client.FirewallRuleUpdate, bool) {
	var body client.FirewallRuleUpdate
	changed := false
	setString := func(field **string, have, wanted string) {
		if have != wanted {
			*field = &wanted
			changed = true
		}
	}
	setString(&body.Description, current.Description, want.Description.ValueString())
	setString(&body.Direction, current.Direction, want.Direction.ValueString())
	setString(&body.Action, current.Action, want.Action.ValueString())
	setString(&body.Protocol, current.Protocol, want.Protocol.ValueString())
	setString(&body.DestinationPort, current.DestinationPort, want.DestinationPort.ValueString())
	setString(&body.SourceCIDR, current.SourceCIDR, want.SourceCIDR.ValueString())
	setString(&body.DestinationCIDR, current.DestinationCIDR, want.DestinationCIDR.ValueString())
	if priority := want.Priority.ValueInt64(); current.Priority != priority {
		body.Priority = &priority
		changed = true
	}
	if enabled := want.Enabled.ValueBool(); current.IsEnabled != enabled {
		body.IsEnabled = &enabled
		changed = true
	}
	return body, changed
}

func (r *instanceFirewallResource) deleteRule(ctx context.Context, c *client.Client, scope client.Scope, instanceID string, rule *client.FirewallRule) error {
	return retryWhileBusy(ctx, r.Data().Waiter, "deleting firewall rule "+rule.Name, func(ctx context.Context) error {
		err := c.DeleteFirewallRule(ctx, scope, instanceID, rule.ID)
		if client.IsNotFound(err) {
			return nil
		}
		return err
	})
}

// recentTaskIDs lists the IDs of the instance's recent tasks, so the tasks queued afterwards can
// be told apart.
func recentTaskIDs(ctx context.Context, c *client.Client, scope client.Scope, instanceID string) (map[string]bool, error) {
	tasks, err := c.ListRecentInstanceTasks(ctx, scope, instanceID)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(tasks))
	for i := range tasks {
		ids[tasks[i].ID] = true
	}
	return ids, nil
}

// applyChanges waits for the apply_firewall the last rule change queued: the newest one not
// listed before the changes (tasks are listed newest first). Each change waited for the previous
// apply to end before it was accepted, so that one reads every rule. When no change queued one
// (nothing changed, or the API skipped the synchronization), the provider applies itself.
func (r *instanceFirewallResource) applyChanges(ctx context.Context, c *client.Client, scope client.Scope, instanceID string, before map[string]bool) error {
	tasks, err := c.ListRecentInstanceTasks(ctx, scope, instanceID)
	if err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].TaskType == client.TaskTypeApplyFirewall && !before[tasks[i].ID] {
			_, err := waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, tasks[i].ID)
			return err
		}
	}
	return r.apply(ctx, c, scope, instanceID)
}

// apply queues one application of the firewall, retried while the synchronizations the rule
// changes queued still run, and waits for it.
func (r *instanceFirewallResource) apply(ctx context.Context, c *client.Client, scope client.Scope, instanceID string) error {
	var taskID string
	err := retryWhileBusy(ctx, r.Data().Waiter, "applying the firewall of instance "+instanceID, func(ctx context.Context) error {
		var err error
		taskID, err = c.ApplyFirewall(ctx, scope, instanceID)
		return err
	})
	if err != nil {
		return err
	}
	_, err = waiter.InstanceTask(ctx, r.Data().Waiter, c, scope, instanceID, taskID)
	return err
}
