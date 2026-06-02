variable "obj" {
  default = { foo = 100, bar = "x" }
}
variable "enabled" {
  default = true
}
resource "r" "a" {
  attr    = var.obj.foo
  mode    = var.enabled ? "on" : "off"
  cmp     = var.obj.foo == 100
}
