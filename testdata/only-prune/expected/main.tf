locals {
  name = "app"
  port = 8080
}
resource "foo" "bar" {
  region = "us-east-1"
  name   = local.name
  port   = local.port
}
