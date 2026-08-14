# Jenkins credentials

Create credentials in Jenkins, rather than putting secrets in this repository:

- `blog-build-callback-url`: callback URL supplied to the site job.
- `blog-build-callback-secret`: HMAC secret shared with the Service.
- `blog-service-health-url`: internal Service health URL.
- SSH agent/key: access for `root@blogweb1` and `root@ngx1` (or an equivalent Jenkins SSH configuration).
- `BLOG_BUILD_TOKEN` is a Jenkins secret text credential used when downloading a release bundle.

Restrict each credential to the blog jobs. Jenkins log masking must be enabled. Rotate the OSS/GFS and
callback secrets if they have ever appeared in shell history or old build logs.
