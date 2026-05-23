locals {
  used   = "U"
  unused = "X"
  needs  = local.undefined
}
resource "foo" "bar" {
  a = local.used
  b = local.needs
}
