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
  region = "us-east-1"
  name   = local.name
  port   = 8080
  host   = var.host
}
