locals {
  region = "us-east-1"
  name   = "app"
  port   = 8080
  secret = "shh"
}
resource "foo" "bar" {
  region = local.region
  name   = local.name
  port   = local.port
  secret = local.secret
}
