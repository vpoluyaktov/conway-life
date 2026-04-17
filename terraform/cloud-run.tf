resource "google_cloud_run_v2_service" "conway_life" {
  name     = var.service_name
  location = var.region
  project  = var.project_id

  template {
    service_account = google_service_account.conway_life.email

    scaling {
      min_instance_count = var.min_instances
      max_instance_count = var.max_instances
    }

    containers {
      image = "gcr.io/${var.project_id}/conway-life:${var.image_tag}"

      ports {
        container_port = 8080
      }

      env {
        name  = "ENVIRONMENT"
        value = var.environment
      }

      env {
        name  = "APP_VERSION"
        value = var.image_tag
      }

      env {
        name  = "GCP_PROJECT_ID"
        value = var.project_id
      }

      env {
        name  = "FIRESTORE_DATABASE_NAME"
        value = var.firestore_database_name
      }

      resources {
        limits = {
          cpu    = var.cpu_limit
          memory = var.memory_limit
        }
        cpu_idle = true
      }
    }

    timeout = var.timeout
  }

  traffic {
    percent = 100
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
  }

  depends_on = [
    google_project_service.apis,
    google_project_iam_member.conway_life_firestore,
    google_firestore_database.main,
  ]
}

resource "google_cloud_run_service_iam_member" "public_access" {
  location = google_cloud_run_v2_service.conway_life.location
  project  = google_cloud_run_v2_service.conway_life.project
  service  = google_cloud_run_v2_service.conway_life.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}
