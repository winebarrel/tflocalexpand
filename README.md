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
  -h, --help                 Show help.
  -i, --in-place             Write changes back to files instead of stdout.
  -p, --prune                Expand inside locals blocks and remove local definitions with no remaining references.
  -e, --eval                 Fold expressions that become statically evaluatable after local substitution (attribute/index accesses, ternaries with a constant condition, comparison/logical operators with constant operands).
      --only=ONLY,...        Only expand these local names; leave others as 'local.<name>' references. Repeat or comma-separate. Mutually exclusive with --except.
      --except=EXCEPT,...    Do not expand these local names; expand the rest. Repeat or comma-separate. Mutually exclusive with --only.
  -v, --verbose              Verbose logging.
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

## Folding static expressions

With `-e` / `--eval`, expressions that become statically evaluatable after substitution are folded to literals:

- A `local.<name>` followed by a chain of `.attr` / `[idx]` accessors is evaluated against the substituted value (e.g. `local.obj.foo` → `100`).
- A ternary whose condition reduces to a constant boolean collapses to the chosen branch (e.g. `local.enabled ? "on" : "off"` with `local.enabled = true` → `"on"`). The branch that isn't taken can reference unknowns; only the condition needs to be evaluatable.
- Comparison (`==`, `!=`, `<`, `<=`, `>`, `>=`) and logical (`&&`, `||`, `!`) operators whose operands are all constants collapse to `true` / `false` (e.g. `"aaa" == "aaa"` → `true`, `100 > 0` → `true`).

Anything that still needs a runtime context (`var.X`, function calls, unresolved locals, etc.) falls back to plain token substitution.

```hcl
# main.tf
locals {
  obj     = { foo = 100, bar = "x" }
  arr     = [1, 2, 3]
  enabled = true
}
resource "r" "a" {
  attr      = local.obj.foo
  index     = local.arr[0]
  fallback  = local.obj.foo + var.something
  mode      = local.enabled ? "on" : "off"
  dead      = 0 > 0 ? foo.bar.zoo : null
  not_const = var.thing ? "x" : "y"
  cmp       = local.obj.foo == 100
  logical   = local.enabled && (10 > 1)
}
```

```sh
tflocalexpand -i -e .
```

```hcl
# main.tf (rewritten)
locals {
  obj     = { foo = 100, bar = "x" }
  arr     = [1, 2, 3]
  enabled = true
}
resource "r" "a" {
  attr      = 100
  index     = 1
  fallback  = 100 + var.something
  mode      = "on"
  dead      = null
  not_const = var.thing ? "x" : "y"
  cmp       = true
  logical   = true
}
```

## Selecting which locals to expand

Pass `--only` to expand only the listed local names; any other `local.<name>` reference is left untouched. Pass `--except` to do the opposite — expand everything except the listed names. The two flags are mutually exclusive, and both accept multiple values (repeat the flag or comma-separate, e.g. `--only region,name`).

```hcl
# main.tf
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
```

```sh
tflocalexpand -i --only region,name .
```

```hcl
# main.tf (rewritten)
locals {
  region = "us-east-1"
  name   = "app"
  port   = 8080
  secret = "shh"
}
resource "foo" "bar" {
  region = "us-east-1"
  name   = "app"
  port   = local.port
  secret = local.secret
}
```

Combining with `--prune`: locals whose references remain (because they were filtered out by `--only`/`--except`) count as "used" and are kept. Locals that got fully expanded and are no longer referenced anywhere are still removed.

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
