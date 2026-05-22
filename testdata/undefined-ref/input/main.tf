locals {
  a = local.undefined
}

resource "r" "n" {
  v = local.a
}
