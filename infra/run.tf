resource "google_cloud_run_v2_service" "app" {
  count    = var.service_enabled ? 1 : 0
  name     = local.service_name
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  deletion_protection = false

  template {
    service_account = google_service_account.run.email
    # Longer than the server's own body read and handler timeouts, two
    # minutes each in main.go, which bound every request, so the server
    # rather than Cloud Run answers a slow request.
    timeout = "300s"
    # Scale to zero and run at most one instance, to stay within the free
    # tier and bound costs.
    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }
    max_instance_request_concurrency = 80

    containers {
      image = local.image
      ports {
        container_port = 8080
      }
      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        # Only use CPU during requests (request based billing).
        cpu_idle          = true
        startup_cpu_boost = true
      }

      dynamic "env" {
        for_each = {
          # A soft limit on the Go runtime's memory, so that it collects
          # garbage harder rather than exceed the instance's 512 MiB, which
          # restarts the only instance. The rest is for what Go doesn't
          # count: the binary, with the embedded frontend, and files in the
          # in-memory /tmp.
          GOMEMLIMIT              = "400MiB"
          FOODPLANNING_HOSTNAME   = "0.0.0.0"
          FOODPLANNING_PUBLIC_URL = local.public_url
          FOODPLANNING_LOG_FORMAT = "json"
          FOODPLANNING_LOG_LEVEL  = "info"
          # Errors are reported to Error Reporting rather than logged.
          FOODPLANNING_ERROR_REPORTING_PROJECT = var.project
          FOODPLANNING_OTLP_ENDPOINT           = "https://telemetry.googleapis.com"
          FOODPLANNING_TRACE_PROJECT           = var.project
          FOODPLANNING_TRACE_SAMPLE_RATE       = tostring(var.trace_sample_rate)
          FOODPLANNING_AVATAR_BUCKET           = google_storage_bucket.avatars.name
        }
        content {
          name  = env.key
          value = env.value
        }
      }

      dynamic "env" {
        for_each = {
          FOODPLANNING_DATABASE_URL          = "database-url"
          FOODPLANNING_ADMIN_BOOTSTRAP_TOKEN = "admin-bootstrap-token"
        }
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.secrets[env.value].secret_id
              version = "latest"
            }
          }
        }
      }
    }
  }

  depends_on = [
    google_secret_manager_secret_iam_member.run,
    google_storage_bucket_iam_member.run_avatars,
    google_project_iam_member.run_error_reporting,
    google_project_iam_member.run_tracing,
  ]
}

# The app handles its own authentication.
resource "google_cloud_run_v2_service_iam_member" "public" {
  count    = var.service_enabled ? 1 : 0
  name     = google_cloud_run_v2_service.app[0].name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Cloud Run provisions and renews a TLS certificate for the domain. The
# domain must be verified, and the first apply with it enabled runs as a
# verified owner, see the README.
resource "google_cloud_run_domain_mapping" "domain" {
  count    = var.service_enabled && local.domain != "" ? 1 : 0
  name     = local.domain
  location = var.region
  metadata {
    namespace = var.project
  }
  spec {
    route_name = google_cloud_run_v2_service.app[0].name
  }
}

# Lets the service report errors to Error Reporting, which is free.
resource "google_project_iam_member" "run_error_reporting" {
  project = var.project
  role    = "roles/errorreporting.writer"
  member  = google_service_account.run.member

  depends_on = [google_project_service.services]
}

# Lets the service export traces, which is free up to 2.5 million spans a
# month.
resource "google_project_iam_member" "run_tracing" {
  project = var.project
  role    = "roles/cloudtrace.agent"
  member  = google_service_account.run.member

  depends_on = [google_project_service.services]
}
