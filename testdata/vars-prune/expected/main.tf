variable "no_default" {
  type = string
}
resource "foo" "bar" {
  name = "u"
  who  = var.no_default
}
