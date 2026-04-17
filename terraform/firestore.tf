# Firestore database (Native mode). No seed documents — sessions are user-created.
resource "google_firestore_database" "main" {
  project     = var.project_id
  name        = var.firestore_database_name
  location_id = var.firestore_location
  type        = "FIRESTORE_NATIVE"

  depends_on = [google_project_service.apis]
}
