terraform {
  backend "gcs" {
    bucket = "dfh-stage-tfstate"
    prefix = "conway-life/state"
  }
}
