locals {
  region = "us-east-1"
  name   = "app"
  port   = 8080
  secret = "shh"
}
resource "foo" "bar" {
  region = "us-east-1"
  name   = "app"
  port   = local.port
  secret = local.secret
}
