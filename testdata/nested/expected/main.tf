locals {
  env = "prod"
}

resource "x" "y" {
  tags = {
    Env  = "prod"
    Tier = "web"
  }
}
