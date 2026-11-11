variable "wordpress_admin_password" {
  type      = string
  sensitive = true
}

# WordPress needs a recent PHP version on its domain: set it first.
resource "onetsolutions_hosting_php_version" "site" {
  hosting_id  = onetsolutions_web_hosting.site.id
  php_version = "ea-php83"
}

resource "onetsolutions_hosting_wordpress" "site" {
  hosting_id = onetsolutions_hosting_php_version.site.hosting_id
  domain     = onetsolutions_hosting_php_version.site.domain
  path       = "blog"
  title      = "Our blog"
  language   = "fr"

  # Applied at installation only.
  admin_user     = "editor"
  admin_password = var.wordpress_admin_password
  admin_email    = "webmaster@example.fr"

  # Change the value to update WordPress to its latest version.
  update_trigger = "2026-11"

  deletion_protection = true
}
