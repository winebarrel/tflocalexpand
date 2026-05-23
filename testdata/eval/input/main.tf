locals {
  obj    = { foo = 100, bar = "x" }
  arr    = [1, 2, 3]
  nested = { a = { b = 7 } }
}
resource "r" "a" {
  attr      = local.obj.foo
  index     = local.arr[0]
  chained   = local.nested.a.b
  fallback  = local.obj.foo + var.something
  no_chain  = local.arr
  not_found = local.obj.foo.missing
  interp    = "${local.obj.bar}-${local.arr[2]}"
}
