locals {
  enabled = true
  size    = 0
}
resource "r" "a" {
  literal_const = "a"
  via_local     = "on"
  dead_branch   = null
  size_check    = "small"
  not_static    = var.thing ? "x" : "y"
  nested_inner  = var.x ? ("ok") : "fallback"
}
