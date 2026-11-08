# The repository and the Actions variables existed before this
# configuration, so they're imported rather than created. Once they're in the
# state, these blocks do nothing.

import {
  to = github_repository.this
  id = var.repository
}

import {
  to = github_repository_dependabot_security_updates.this
  id = var.repository
}

import {
  for_each = toset(["GCP_PROJECT_ID", "GCP_PROJECT_NUMBER"])
  to       = github_actions_variable.gcp[each.key]
  id       = "${var.repository}:${each.key}"
}
