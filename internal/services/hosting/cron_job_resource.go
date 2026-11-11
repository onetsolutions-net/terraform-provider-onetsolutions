package hosting

import (
	"context"
	"fmt"
	"regexp"

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
	_ resource.ResourceWithConfigure   = (*cronJobResource)(nil)
	_ resource.ResourceWithImportState = (*cronJobResource)(nil)
)

// One crontab field, such as */5, 0 or 1-5: a whole expression in one field would shift the
// command and break the account's crontab.
var cronFieldPattern = regexp.MustCompile(`^[0-9A-Za-z*/,-]+$`)

// NewCronJobResource returns the onetsolutions_hosting_cron_job resource.
func NewCronJobResource() resource.Resource {
	return &cronJobResource{}
}

type cronJobResource struct {
	common.ResourceBase
}

type cronJobModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	HostingID      types.String `tfsdk:"hosting_id"`
	Command        types.String `tfsdk:"command"`
	Minute         types.String `tfsdk:"minute"`
	Hour           types.String `tfsdk:"hour"`
	Day            types.String `tfsdk:"day"`
	Month          types.String `tfsdk:"month"`
	Weekday        types.String `tfsdk:"weekday"`
}

func (m *cronJobModel) input() client.HostingCronJobInput {
	return client.HostingCronJobInput{
		Command: m.Command.ValueString(), Minute: m.Minute.ValueString(), Hour: m.Hour.ValueString(),
		Day: m.Day.ValueString(), Month: m.Month.ValueString(), Weekday: m.Weekday.ValueString(),
	}
}

func (m *cronJobModel) fill(job *client.HostingCronJob) {
	m.ID = types.StringValue(job.LineKey)
	m.Command = types.StringValue(job.Command)
	m.Minute = types.StringValue(job.Minute)
	m.Hour = types.StringValue(job.Hour)
	m.Day = types.StringValue(job.Day)
	m.Month = types.StringValue(job.Month)
	m.Weekday = types.StringValue(job.Weekday)
}

func sameCronJob(job *client.HostingCronJob, in client.HostingCronJobInput) bool {
	return job.Command == in.Command && job.Minute == in.Minute && job.Hour == in.Hour &&
		job.Day == in.Day && job.Month == in.Month && job.Weekday == in.Weekday
}

// findCronJob finds the job of the state. Line keys can change when other lines of the crontab
// change, so a job no longer at its key is found again by its command and schedule (when only
// one job matches). On import only the key is known.
func findCronJob(jobs []client.HostingCronJob, m *cronJobModel) *client.HostingCronJob {
	if m.Command.IsNull() {
		for i := range jobs {
			if jobs[i].LineKey == m.ID.ValueString() {
				return &jobs[i]
			}
		}
		return nil
	}
	in := m.input()
	var matches []*client.HostingCronJob
	for i := range jobs {
		if !sameCronJob(&jobs[i], in) {
			continue
		}
		if jobs[i].LineKey == m.ID.ValueString() {
			return &jobs[i]
		}
		matches = append(matches, &jobs[i])
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return nil
}

func (r *cronJobResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hosting_cron_job"
}

func cronField(name, example string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true,
		Computed: true,
		Default:  stringdefault.StaticString("*"),
		MarkdownDescription: fmt.Sprintf("The %s field of the schedule: one value such as `%s`, a range, a list or a step, "+
			"without spaces. Defaults to `*`. Changing it recreates the job.", name, example),
		Validators: []validator.String{
			stringvalidator.RegexMatches(cronFieldPattern, "must be one cron value, such as */5, 0 or 1-5, without spaces"),
		},
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

func (r *cronJobResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a cron job of a web hosting.",
		Attributes: common.MergeAttributes(common.ScopeAttributes(), map[string]schema.Attribute{
			common.AttrID: schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "The line key of the job in the web hosting's crontab. It can change when other jobs " +
					"of the web hosting are added or removed; the job is then found again by its command and schedule.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			attrHostingID: hostingIDAttribute("cron job"),
			"command": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The command to run, such as `php -q /home/account/public_html/cron.php`. Changing it recreates the job.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1), common.NoSurroundingWhitespace()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"minute":  cronField("minute", "*/5"),
			"hour":    cronField("hour", "3"),
			"day":     cronField("day of month", "1"),
			"month":   cronField("month", "1-6"),
			"weekday": cronField("day of week", "1-5"),
		}),
	}
}

func (r *cronJobResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cronJobModel
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
	hostingID, in := plan.HostingID.ValueString(), plan.input()

	before, err := c.ListHostingCronJobs(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the cron jobs of the web hosting", err)
		return
	}
	known := map[string]bool{}
	for i := range before {
		if sameCronJob(&before[i], in) {
			resp.Diagnostics.AddError("Cron job already exists",
				fmt.Sprintf("The web hosting already runs %q on this schedule (line key %s). Import it rather than creating it twice.",
					in.Command, before[i].LineKey))
			return
		}
		known[before[i].LineKey] = true
	}
	if _, err = c.CreateHostingCronJob(ctx, scope, hostingID, in); err != nil {
		common.AddClientErrorWithFields(&resp.Diagnostics, "Could not create the cron job", err, map[string]path.Path{
			"command": path.Root("command"), "minute": path.Root("minute"), "hour": path.Root("hour"),
			"day": path.Root("day"), "month": path.Root("month"), "weekday": path.Root("weekday"),
		})
		return
	}
	after, err := c.ListHostingCronJobs(ctx, scope, hostingID)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the cron jobs of the web hosting", err)
		return
	}
	var created *client.HostingCronJob
	for i := range after {
		if sameCronJob(&after[i], in) && (!known[after[i].LineKey] || created == nil) {
			created = &after[i]
		}
	}
	if created == nil {
		resp.Diagnostics.AddError("Cron job not found",
			"The cron job was created, but the web hosting does not list it. Import it once it is listed.")
		return
	}
	plan.fill(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *cronJobResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cronJobModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	jobs, gone, err := webChildList(ctx, c, webScope(state.OrganizationID, state.ProjectID), state.HostingID.ValueString(), c.ListHostingCronJobs)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the cron jobs of the web hosting", err)
		return
	}
	job := findCronJob(jobs, &state)
	if gone || job == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(job)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called: every argument forces a replacement.
func (r *cronJobResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan cronJobModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *cronJobResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cronJobModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	c := r.Client(&resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	scope, hostingID := webScope(state.OrganizationID, state.ProjectID), state.HostingID.ValueString()
	jobs, gone, err := webChildList(ctx, c, scope, hostingID, c.ListHostingCronJobs)
	if err != nil {
		common.AddClientError(&resp.Diagnostics, "Could not list the cron jobs of the web hosting", err)
		return
	}
	job := findCronJob(jobs, &state)
	if gone || job == nil {
		return
	}
	if err := c.DeleteHostingCronJob(ctx, scope, hostingID, job.LineKey); err != nil {
		webChildDeleteFailed(ctx, c, scope, hostingID, err, &resp.Diagnostics, "Could not delete the cron job")
	}
}

func (r *cronJobResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importWebChild(ctx, &r.ResourceBase, req, resp, "line_key")
}
