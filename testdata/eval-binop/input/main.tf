locals {
  enabled = true
  size    = 0
}
resource "r" "a" {
  literal_eq   = "aaa" == "aaa"
  literal_ne   = "aaa" != "bbb"
  literal_gt   = 100 > 0
  literal_lt   = 0 < 100
  literal_ge   = 5 >= 5
  literal_le   = 5 <= 10
  via_local_eq = local.size == 0
  via_local_gt = local.enabled == true
  and_op       = true && false
  or_op        = false || true
  not_op       = !false
  not_static   = var.x == "a"
  nested_bool  = (1 > 0) && (2 == 2)
  arith_in_cmp = (1 + 2) == 3
  in_template  = "x=${100 > 0}"
}
