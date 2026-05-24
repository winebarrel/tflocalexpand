locals {
  enabled = true
  size    = 0
}
resource "r" "a" {
  literal_const = true ? "a" : "b"
  via_local     = local.enabled ? "on" : "off"
  dead_branch   = 0 > 0 ? foo.bar.zoo : null
  size_check    = local.size > 0 ? "big" : "small"
  not_static    = var.thing ? "x" : "y"
  nested_inner  = var.x ? (true ? "ok" : "ko") : "fallback"
}
