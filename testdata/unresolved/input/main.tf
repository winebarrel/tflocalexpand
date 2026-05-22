locals {
  known = "yes"
}

resource "x" "y" {
  a = local.known
  b = local.missing
}
