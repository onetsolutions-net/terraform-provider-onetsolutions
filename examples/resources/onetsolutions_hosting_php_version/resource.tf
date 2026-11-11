data "onetsolutions_hosting_php_versions" "site" {
  hosting_id = onetsolutions_web_hosting.site.id
}

# The primary domain of the web hosting.
resource "onetsolutions_hosting_php_version" "site" {
  hosting_id  = onetsolutions_web_hosting.site.id
  php_version = "ea-php83"

  lifecycle {
    precondition {
      condition     = contains(data.onetsolutions_hosting_php_versions.site.versions[*].version, "ea-php83")
      error_message = "PHP 8.3 is not offered on this web hosting."
    }
  }
}

# Another domain of the same web hosting.
resource "onetsolutions_hosting_php_version" "legacy" {
  hosting_id  = onetsolutions_web_hosting.site.id
  domain      = "legacy.example.fr"
  php_version = "ea-php81"
}
