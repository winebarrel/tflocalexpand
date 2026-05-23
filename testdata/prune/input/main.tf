locals {
  a = "aaa"
  b = "bbb"
  c = "ccc"
}
locals {
  x = "${local.a}x${local.b}x${local.c}"
}
resource "foo" "bar" {
  name = local.x
}
