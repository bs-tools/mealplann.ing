# Users' avatars. The app serves them itself, at /avatars/<key>.jpg, so the
# bucket isn't public: only the service account can read and write it.
# us-west1 is in Cloud Storage's always free tier (5 GB), and avatars are a
# few kilobytes each.
resource "google_storage_bucket" "avatars" {
  name                        = "${var.project}-avatars"
  location                    = upper(var.region)
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  depends_on = [google_project_service.services]
}

# The service stores, reads and deletes avatars.
resource "google_storage_bucket_iam_member" "run_avatars" {
  bucket = google_storage_bucket.avatars.name
  role   = "roles/storage.objectUser"
  member = google_service_account.run.member
}
