variable "region" {
  default = "us-east-1"
}
locals {
  prefix = "app-${var.region}"
}
resource "foo" "bar" {
  name   = local.prefix
  region = var.region
}
