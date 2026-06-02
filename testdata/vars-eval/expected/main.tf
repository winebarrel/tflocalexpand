variable "obj" {
  default = { foo = 100, bar = "x" }
}
variable "enabled" {
  default = true
}
resource "r" "a" {
  attr = 100
  mode = "on"
  cmp  = true
}
