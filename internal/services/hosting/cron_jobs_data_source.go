package hosting

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"

	"github.com/onetsolutions-net/terraform-provider-onetsolutions/internal/client"
)

// NewCronJobsDataSource returns the onetsolutions_hosting_cron_jobs data source.
func NewCronJobsDataSource() datasource.DataSource {
	return &webListDataSource[client.HostingCronJob]{
		typeName:    "_hosting_cron_jobs",
		description: "Lists the cron jobs of a web hosting.",
		listName:    "cron_jobs",
		listWhat:    "The cron jobs, in the order of the web hosting's crontab.",
		attributes: map[string]string{
			"line_key": "The line key of the job, to import it as an `onetsolutions_hosting_cron_job`.",
			"command":  "The command the job runs.",
			"minute":   "The minute field of the schedule.",
			"hour":     "The hour field of the schedule.",
			"day":      "The day of month field of the schedule.",
			"month":    "The month field of the schedule.",
			"weekday":  "The day of week field of the schedule.",
		},
		list: func(c *client.Client) webListFunc[client.HostingCronJob] { return c.ListHostingCronJobs },
		values: func(j *client.HostingCronJob) map[string]string {
			return map[string]string{
				"line_key": j.LineKey, "command": j.Command, "minute": j.Minute, "hour": j.Hour,
				"day": j.Day, "month": j.Month, "weekday": j.Weekday,
			}
		},
	}
}
