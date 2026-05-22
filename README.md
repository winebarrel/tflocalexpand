# tflocalexpand

[![CI](https://github.com/winebarrel/tflocalexpand/actions/workflows/ci.yml/badge.svg)](https://github.com/winebarrel/tflocalexpand/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/winebarrel/tflocalexpand/branch/main/graph/badge.svg)](https://codecov.io/gh/winebarrel/tflocalexpand)

`tflocalexpand` inlines `local.<name>` references in Terraform `.tf` files with the expression from the corresponding `locals { ... }` block. The `locals` blocks themselves are left untouched — only the reference sites are substituted.

## Installation

```
go install github.com/winebarrel/tflocalexpand/cmd/tflocalexpand@latest
```

## Usage

```
Usage: tflocalexpand [<dir>] [flags]

Expand local.<name> references in Terraform .tf files.

Arguments:
  [<dir>]    Directory containing *.tf files (default: ".").

Flags:
  -h, --help        Show help.
  -i, --in-place    Write changes back to files instead of stdout.
  -v, --verbose     Verbose logging.
      --version
```

By default the result is printed to stdout. Pass `-i` / `--in-place` to rewrite the files on disk.

## Example

```hcl
# main.tf
locals {
  region = "us-east-1"
  base   = 2
  scaled = local.base * 10
  prefix = "app"
}

resource "foo" "bar" {
  region = local.region
  count  = local.scaled
  name   = "${local.prefix}-server"
}
```

```sh
tflocalexpand -i .
```

```hcl
# main.tf (rewritten)
locals {
  region = "us-east-1"
  base   = 2
  scaled = local.base * 10
  prefix = "app"
}

resource "foo" "bar" {
  region = "us-east-1"
  count  = (2 * 10)
  name   = "app-server"
}
```

Notes:

- Chained locals (`local.scaled` referencing `local.base`) are resolved transitively.
- Substituted expressions are wrapped in parentheses when needed to preserve operator precedence (`local.base * 10` above).
- `${...}` interpolation collapses when the inner value is a string, number, or boolean literal (`"${local.prefix}-server"` → `"app-server"`).
- References to undefined locals are left as-is.
- Circular references are reported as errors.
