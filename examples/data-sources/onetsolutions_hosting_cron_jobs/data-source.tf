data "onetsolutions_web_hosting" "site" {
  domain = "example.com"
}

data "onetsolutions_hosting_cron_jobs" "site" {
  hosting_id = data.onetsolutions_web_hosting.site.id
}

# The line keys to import existing jobs as onetsolutions_hosting_cron_job.
output "cron_jobs" {
  value = {
    for job in data.onetsolutions_hosting_cron_jobs.site.cron_jobs :
    job.line_key => "${job.minute} ${job.hour} ${job.day} ${job.month} ${job.weekday} ${job.command}"
  }
}
