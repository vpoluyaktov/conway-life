terraform {
  backend "gcs" {
    bucket = "dfh-prod-tfstate"
    prefix = "conway-life/state"
  }
}
