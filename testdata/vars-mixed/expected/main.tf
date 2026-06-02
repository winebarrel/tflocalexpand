variable "region" {
  default = "us-east-1"
}
locals {
  prefix = "app-${var.region}"
}
resource "foo" "bar" {
  name   = "app-us-east-1"
  region = "us-east-1"
}
