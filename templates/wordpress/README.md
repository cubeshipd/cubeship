# WordPress on Cubeship

[WordPress](https://wordpress.org) is the open-source publishing platform
behind a large share of the web: a site or a blog, extended with themes and
plugins.

This template installs it on a Cubeship instance with the managed MySQL it
needs, and a volume for everything it keeps in files.

## What it creates

- **wordpress** — WordPress, from `wordpress:7.1.0-php8.3-apache`, answering
  on the domain you choose, with a volume at `/var/www/html`: WordPress
  itself, `wp-config.php` with the site's secret keys, and `wp-content` —
  plugins, themes and uploads.
- **wordpress-db** — a managed MySQL 8.4 database, attached to the app.
  Posts, pages, users, comments and settings are in it.

It needs Cubeship 0.7.0 or newer.

## What you are asked

| Input | What to give |
| --- | --- |
| Where the site answers | A domain you control, pointed at your instance. |

The secret keys and salts need no answer: the image writes random ones into
`wp-config.php` on the first start, and the volume keeps them.

## After installing

1. **Open the domain straight away.** WordPress has no default account: the
   first person to open it runs the install wizard and creates the admin.
   Until you do, that is anyone who finds the domain.
2. Install an SMTP plugin, such as *WP Mail SMTP*, and give it your mail
   provider's settings. The image cannot send mail on its own, so password
   resets and notifications go nowhere until you do.

The site's address is fixed to `https://<your domain>` by
`WORDPRESS_CONFIG_EXTRA` on the `wordpress` app, which is why *WordPress
Address* and *Site Address* are greyed out under *Settings → General*.
Change the variable if the domain changes, and redeploy.

## Updating WordPress

Update WordPress, plugins and themes from its own dashboard. The image copies
WordPress into the volume only when the volume is empty, so a newer `tag`
changes PHP and Apache, not the WordPress the site runs.

## Uploads larger than 2 MB

PHP's default limit is 2 MB per file. Raise it with `.htaccess` in the volume,
over SSH on the machine the app runs on, since Cubeship has no console into
an app:

```bash
docker exec $(docker ps -qf name=cubeship-wordpress-production-wordpress) \
  sh -c 'printf "php_value upload_max_filesize 64M\nphp_value post_max_size 64M\n" >> .htaccess'
```

It applies from the next request. WordPress only rewrites the lines between
its own `# BEGIN WordPress` and `# END WordPress` markers, so these stay.

## Scheduled tasks

WP-Cron runs on page views: WordPress calls its own `wp-cron.php` over the
domain, so there is nothing to schedule and no `DISABLE_WP_CRON` to set. On a
site with few visitors, scheduled posts go out at the next visit rather than
on the minute.

## The volume

The app runs as one copy on the machine its volume is on, and a deploy stops
it for a few seconds. Back up both the volume, for plugins, themes and
uploads, and the database, for everything else: one without the other is a
site with broken images or no posts.

## Resources

The app is limited to 1 CPU and 1 GiB of memory. Raise `limits` in
`template.yaml` for a busy site.
