locals {
  obj    = { foo = 100, bar = "x" }
  arr    = [1, 2, 3]
  nested = { a = { b = 7 } }
}
resource "r" "a" {
  attr      = 100
  index     = 1
  chained   = 7
  fallback  = 100 + var.something
  no_chain  = [1, 2, 3]
  not_found = { foo = 100, bar = "x" }.foo.missing
  interp    = "x-3"
}
