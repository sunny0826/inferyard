# `function_size`

[Back to the checker index](../checkers.md)

Reports function metric violations. One checker owns all five function
diagnostics, `size.lines`, `size.statements`, `size.complexity`, `size.nesting`
and `size.parameters`, and its single `enabled` switch controls all of them.

**Severity**: error for every one of the five diagnostics.

**Languages**: every analyzer.

Metrics are syntax metrics, not compiler control flow graphs, so case and
exception semantics differ between languages. Complexity starts at one and
increments for recognized branches, loops, catches, non-default cases,
conditional expressions, comprehension decisions and short circuit operators.
Nesting counts nested recognized control constructs; Python `elif` alternatives
share their parent's depth. Blank lines, comments and Python docstrings are
excluded from the line span, while signatures and nested bodies are counted.
Nested functions receive independent control-flow checks and do not inflate the
enclosing function's complexity. Formatting and line length stay language-tool
checks.

| Language | Language-specific metric rules |
| --- | --- |
| Go and Python | Go grouped parameter names count separately, and Go receivers plus Python `self`/`cls` do not consume the argument budget. |

#### Java

Java measures methods, constructors, compact record constructors and lambdas; its
receivers do not consume the argument budget and varargs count once. Its control
metrics count branches, loops including enhanced `for`, catches, non-default
switch labels, ternaries and short-circuit operators, with nested lambdas
measured independently.

## Configuration

**Options**: `enabled` (boolean, default `true`); `lines` (integer, at least 1,
default 50); `statements` (integer, at least 1, default 50); `complexity`
(integer, at least 1, default 10); `nesting` (integer, at least 1, default 3);
`parameters` (integer, at least 1, default 5). Maxima are inclusive; strings,
booleans and fractions are rejected.

Correct — per-metric thresholds are checker options and may be set one by one:

```json
{
  "checkers": {
    "function_size": {"lines": 40, "complexity": 8}
  }
}
```

Incorrect — the five thresholds are named `lines`, `statements`, `complexity`,
`nesting` and `parameters`; `max_lines` is not one of them, and a separate
`size.lines` checker entry is rejected because the five `size.*` diagnostics are
not independently configurable:

```json
{
  "checkers": {
    "function_size": {"max_lines": 40},
    "size.lines": {"maximum": 40}
  }
}
```

An approved next-line exception for one metric names that diagnostic ID, for
example `size.lines`, never the `function_size` checker key.
