variable "region" {
  default = "us-east-1"
}
variable "name" {
  default = "app"
}
variable "no_default" {
  type = string
}
resource "foo" "bar" {
  region = "us-east-1"
  name   = "app"
  who    = var.no_default
}
