locals {
  base   = 2
  scaled = local.base * 10
}

resource "x" "y" {
  count = local.scaled
}
