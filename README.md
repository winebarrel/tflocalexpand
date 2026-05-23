# tflocalexpand

[![CI](https://github.com/winebarrel/tflocalexpand/actions/workflows/ci.yml/badge.svg)](https://github.com/winebarrel/tflocalexpand/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/winebarrel/tflocalexpand/branch/main/graph/badge.svg)](https://codecov.io/gh/winebarrel/tflocalexpand)
[![AI Generated](https://img.shields.io/badge/AI%20Generated-Claude-orange?logo=anthropic)](https://claude.ai/claude-code)

`tflocalexpand` inlines `local.<name>` references in Terraform `.tf` files with the expression from the corresponding `locals { ... }` block. The `locals` blocks themselves are left untouched — only the reference sites are substituted.

## Installation

```
brew install winebarrel/tflocalexpand/tflocalexpand
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
  -p, --prune       Expand inside locals blocks and remove local definitions with no remaining references.
  -e, --eval        Fold direct attribute/index accesses on a substituted local (e.g. local.obj.foo) to a literal when fully evaluatable.
  -v, --verbose     Verbose logging.
      --version
```

By default the result is printed to stdout. Pass `-i` / `--in-place` to rewrite the files on disk.

Pass `-p` / `--prune` to also expand `local.<name>` references *inside* `locals` blocks and then delete any local definition that no longer has a reference anywhere. Locals whose references could not be fully expanded (e.g., pointing at an undefined name) are kept.

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

## Folding direct accesses

With `-e` / `--eval`, when a `local.<name>` reference is immediately followed by a chain of `.attr` / `[idx]` accessors, the substituted value plus the chain is evaluated as a constant expression. If it yields a concrete value, the whole reference collapses to a literal. Anything that needs a runtime context (`var.X`, function calls, etc.) falls back to the plain token substitution.

```hcl
# main.tf
locals {
  obj = { foo = 100, bar = "x" }
  arr = [1, 2, 3]
}
resource "r" "a" {
  attr      = local.obj.foo
  index     = local.arr[0]
  fallback  = local.obj.foo + var.something
}
```

```sh
tflocalexpand -i -e .
```

```hcl
# main.tf (rewritten)
locals {
  obj = { foo = 100, bar = "x" }
  arr = [1, 2, 3]
}
resource "r" "a" {
  attr      = 100
  index     = 1
  fallback  = 100 + var.something
}
```

## Pruning unused locals

With `-p` / `--prune`, references inside `locals` blocks are expanded too, and any local whose `local.<name>` is no longer referenced anywhere is removed. Empty `locals` blocks are dropped.

```hcl
# main.tf
locals {
  a = "aaa"
  b = "bbb"
  c = "ccc"
}
locals {
  x = "${local.a}x${local.b}x${local.c}"
}
resource "foo" "bar" {
  name = local.x
}
```

```sh
tflocalexpand -i -p .
```

```hcl
# main.tf (rewritten)
resource "foo" "bar" {
  name = "aaaxbbbxccc"
}
```
