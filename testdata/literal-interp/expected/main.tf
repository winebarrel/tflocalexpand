locals {
  num   = 42
  ratio = 1.5
  on    = true
  off   = false
}

resource "r" "n" {
  a = "n-42"
  b = "r-1.5"
  c = "true/false"
}
