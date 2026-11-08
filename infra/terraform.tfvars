# Replace the placeholders once the Google Cloud project exists, see
# "Bootstrapping the project" in the README. The project ID is also in the
# state bucket's name in versions.tf.
project         = "foodplanning-514023"
region          = "us-west1"
billing_account = "011594-64C1DB-C0AD77"
budget_amount   = 1
budget_currency = "CAD"
domain          = "mealplann.ing"
# The previous domain, which redirects to the new one.
redirect_domains = ["foodplann.ing"]
# The ID of bs-tools/mealplann.ing (the same as before it was renamed from
# bs-tools/foodplann.ing), from
# gh api repos/bs-tools/mealplann.ing --jq .id
github_repository_id = "1449424396"
# Enable once the domain is verified, applying locally as a verified owner.
# See the README.
domain_enabled = true
