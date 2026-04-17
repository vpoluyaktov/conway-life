# Cloud Run domain mapping is created once manually via gcloud (requires domain
# verification in Google Search Console for the calling identity):
#
#   gcloud beta run domain-mappings create \
#     --service=<service_name> \
#     --domain=<custom_domain> \
#     --region=<region> \
#     --project=<project_id>
#
# After creation, Terraform manages only the DNS CNAME record below.

resource "google_dns_record_set" "cloud_run_cname" {
  project      = var.dns_project_id
  managed_zone = var.dns_zone_name
  name         = "${var.custom_domain}."
  type         = "CNAME"
  ttl          = 300
  rrdatas      = ["ghs.googlehosted.com."]
}
