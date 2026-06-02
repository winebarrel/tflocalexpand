locals {
  region = "us-east-1"
  name   = "app"
}
variable "port" {
  default = 8080
}
variable "host" {
  default = "h"
}
resource "foo" "bar" {
  region = local.region
  name   = local.name
  port   = var.port
  host   = var.host
}
