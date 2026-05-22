locals {
  num   = 42
  ratio = 1.5
  on    = true
  off   = false
}

resource "r" "n" {
  a = "n-${local.num}"
  b = "r-${local.ratio}"
  c = "${local.on}/${local.off}"
}
