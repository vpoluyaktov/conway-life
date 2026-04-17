resource "google_service_account" "conway_life" {
  account_id   = "conway-life-${var.environment}"
  display_name = "conway-life Runtime Service Account (${title(var.environment)})"
  project      = var.project_id
}

resource "google_project_iam_member" "conway_life_logging" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.conway_life.email}"
}

resource "google_project_iam_member" "conway_life_firestore" {
  project = var.project_id
  role    = "roles/datastore.user"
  member  = "serviceAccount:${google_service_account.conway_life.email}"
}
