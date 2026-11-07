# Infrastructure

Terraform for the Google Cloud project: the Cloud Run service, the avatars
bucket, secrets, the budget, and the service accounts for the app and GitHub
Actions. Merging to `main` builds the image with ko and runs `terraform
apply`. Pull requests that change this directory get a plan as a comment.
Both need the repository variables `GCP_PROJECT_ID` and `GCP_PROJECT_NUMBER`.

## Cost

The app is meant to stay within the free tiers. Keep it that way, and explain
in a comment any resource that could cost money:

- The Cloud Run service scales to zero, runs at most one instance with 1 CPU
  and 512 MiB, and only uses CPU during requests.
- Buckets are in `us-west1` for the free tier.
- Tracing has no custom metrics; Cloud Run's metrics are enough.
- The budget alerts at 50%, 90% and 100% of the monthly budget.

## Style

- Format with `terraform fmt`; CI checks it and runs `terraform validate`.
- Comment each resource with why it exists or why it's set up that way: cost,
  free tier, security, or what in the app depends on it.
- Give every variable a description.
- Put derived values in `locals`. Make optional resources depend on a
  variable or local with `count = ... ? 1 : 0`, and repeat resources with
  `for_each`.
- Add `depends_on = [google_project_service.services]` to resources that need
  an API enabled, and enable new APIs in that list.
- Grant the narrowest role on the narrowest resource, e.g. a role on one
  bucket rather than the project.

## Configuration

- The server reads each flag `-foo-bar` from the environment variable
  `FOODPLANNING_FOO_BAR`. Set plain values in the service's `env` map, and
  secrets in the map from variable to secret ID.
- Secrets are created here without values, which are added with `gcloud
  secrets versions add` so they never enter the Terraform state.
- CI sets `var.image` to the image it built. When it's empty, the deployed
  image is kept, so local plans don't roll it back.
- When a timeout here must match one in the app, like the service's request
  timeout and the server's handler timeout, say so in comments on both.
