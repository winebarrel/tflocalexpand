locals {
  known = "yes"
}

resource "x" "y" {
  a = "yes"
  b = local.missing
}
