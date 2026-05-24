locals {
  enabled = true
  size    = 0
}
resource "r" "a" {
  literal_eq   = true
  literal_ne   = true
  literal_gt   = true
  literal_lt   = true
  literal_ge   = true
  literal_le   = true
  via_local_eq = true
  via_local_gt = true
  and_op       = false
  or_op        = true
  not_op       = true
  not_static   = var.x == "a"
  nested_bool  = true
  arith_in_cmp = true
  in_template  = "x=true"
}
