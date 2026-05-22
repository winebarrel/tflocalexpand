locals {
  region = "us-east-1"
}

resource "foo" "bar" {
  region = local.region
}
