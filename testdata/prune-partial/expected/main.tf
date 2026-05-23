resource "foo" "bar" {
  a = "U"
  b = (local.undefined)
}
