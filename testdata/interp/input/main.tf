locals {
  prefix = "app"
}

resource "x" "y" {
  name = "${local.prefix}-server"
}
