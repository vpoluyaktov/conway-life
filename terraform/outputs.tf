output "service_url" {
  description = "URL of the Cloud Run service"
  value       = google_cloud_run_v2_service.conway_life.uri
}

output "service_account_email" {
  description = "Runtime service account email"
  value       = google_service_account.conway_life.email
}

output "project_id" {
  description = "GCP project ID"
  value       = var.project_id
}

output "region" {
  description = "GCP region"
  value       = var.region
}

output "custom_domain_url" {
  description = "Custom domain URL for the Cloud Run service"
  value       = "https://${var.custom_domain}"
}

output "firestore_database_name" {
  description = "Firestore database name"
  value       = google_firestore_database.main.name
}
