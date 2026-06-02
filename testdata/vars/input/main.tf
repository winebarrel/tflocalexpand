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
  region = var.region
  name   = var.name
  who    = var.no_default
}
