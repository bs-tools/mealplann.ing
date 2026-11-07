# Replace the placeholders once the Google Cloud project exists, see
# "Bootstrapping the project" in the README. The project ID is also in the
# state bucket's name in versions.tf.
project         = "REPLACE-WITH-PROJECT-ID"
region          = "us-west1"
billing_account = "REPLACE-WITH-BILLING-ACCOUNT-ID"
budget_amount   = 1
budget_currency = "CAD"
domain          = "foodplann.ing"
# The ID of bs-tools/foodplann.ing, from
# gh api repos/bs-tools/foodplann.ing --jq .id
github_repository_id = "1449424396"
# Enable once the domain is verified, applying locally as a verified owner.
# See the README.
domain_enabled = false
