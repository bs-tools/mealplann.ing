owner       = "bs-tools"
repository  = "mealplann.ing"
description = "Food planning software"
visibility  = "private"
project     = "foodplanning-514023"
# The jobs in .github/workflows/ci.yml that check every change. The deploy
# and Dependabot jobs aren't checks.
required_checks = [
  "Check diff after formatting code",
  "Run linters",
  "Check diff after regenerating code",
  "Build Go code (stable)",
  "Build Go code (oldstable)",
  "Run Go tests",
  "Check diff after tidying go.mod and go.sum",
  "Check Terraform",
]
