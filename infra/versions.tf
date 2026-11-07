terraform {
  required_version = ">= 1.14"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.6"
    }
  }

  # The state bucket is created once by hand, see the README. Backend
  # blocks can't use variables, so replace the project here too.
  backend "gcs" {
    bucket = "foodplanning-514023-tfstate"
    prefix = "foodplanning"
  }
}

provider "google" {
  project = var.project
  region  = var.region
  # Budgets are billing account resources, and their API needs a project
  # to bill requests to.
  user_project_override = true
  billing_project       = var.project
}

data "google_project" "this" {}
