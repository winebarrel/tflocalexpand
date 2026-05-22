locals {
  env = "prod"
}

resource "x" "y" {
  tags = {
    Env  = local.env
    Tier = "web"
  }
}
