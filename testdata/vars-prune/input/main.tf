variable "used" {
  default = "u"
}
variable "unused" {
  default = "ig"
}
variable "no_default" {
  type = string
}
resource "foo" "bar" {
  name = var.used
  who  = var.no_default
}
