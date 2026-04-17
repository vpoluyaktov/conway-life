variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "service_name" {
  description = "Name of the Cloud Run service"
  type        = string
}

variable "environment" {
  description = "Environment name (staging, production)"
  type        = string
}

variable "image_tag" {
  description = "Docker image tag to deploy"
  type        = string
  default     = "latest"
}

variable "min_instances" {
  description = "Minimum number of Cloud Run instances"
  type        = number
  default     = 0
}

variable "max_instances" {
  description = "Maximum number of Cloud Run instances"
  type        = number
  default     = 3
}

variable "cpu_limit" {
  description = "CPU limit for Cloud Run service"
  type        = string
  default     = "1"
}

variable "memory_limit" {
  description = "Memory limit for Cloud Run service"
  type        = string
  default     = "512Mi"
}

variable "timeout" {
  description = "Request timeout for Cloud Run service"
  type        = string
  default     = "30s"
}

variable "tfstate_bucket_name" {
  description = "Name of the GCS bucket for Terraform state"
  type        = string
}

variable "dns_project_id" {
  description = "GCP project ID where Cloud DNS zone is managed"
  type        = string
}

variable "dns_zone_name" {
  description = "Cloud DNS managed zone name"
  type        = string
}

variable "dns_domain" {
  description = "Base DNS domain (e.g., demo.devops-for-hire.com)"
  type        = string
}

variable "custom_domain" {
  description = "Full custom domain for the Cloud Run service"
  type        = string
}

variable "firestore_database_name" {
  description = "Firestore database name"
  type        = string
}

variable "firestore_location" {
  description = "Firestore database location"
  type        = string
  default     = "nam5"
}
