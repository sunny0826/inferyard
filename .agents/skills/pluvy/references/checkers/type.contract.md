# `type.contract`

[Back to the checker index](../checkers.md)

Reports nullable contracts and union types that exceed the configured aggregate
limits in TypeScript and TSX. Both findings are errors: they identify a contract
that admits more shapes than the policy allows, not a runtime failure.

**Severity**: error.

**Languages**: TypeScript and TSX only.

`type.nullable` aggregates two independent signals. Within one object type or
interface declaration it counts directly declared property signatures that are
optional (`?`) or explicitly nullable, meaning a `null` or `undefined` union
member or that annotation itself. A member that is both optional and nullable
counts once, and a nested object literal type forms its own declaration domain.
Within one innermost function body it counts `??` binary expressions whose right
operand is a bare `null` or `undefined` literal. Module-level expressions do not
count, and a nested function body belongs to that function rather than its
enclosing body. Each signal emits one finding anchored at its first counted
member or `??` expression when the count exceeds `optional_fields` or
`null_defaults`.

`type.union` evaluates each union expression, peeling one `parenthesized_type`
layer from its members and removing `null` and `undefined` members before
counting. A parenthesized union is a separate expression and is judged on its
own. The declaration signal likewise peels at most one layer, so a doubly
parenthesized nullable annotation is not recognized. `unknown` and `any` members
remain in the union count and continue to be reported by `type.broad` at the
member node, so both diagnostics can report the same type. A member such as
`Record<string, unknown>` likewise reports `type.broad` at the `unknown` type
argument.

A `??` whose right operand is wrapped in parentheses, an assertion, or a call is
not counted by `type.nullable`; such an expression remains a candidate for
`type.assertion` and `type.broad`. A `?? {}` object literal is not a nullish
fallback and does not trigger the `type.broad` empty-object branch, because the
fallback is an object literal rather than an `object_type` annotation. Optional
function parameters are not property declarations and do not participate in the
declaration signal, while a standalone optional chain such as `x?.y` is not a
nullish fallback.

Thresholds use a strict `>`: a count equal to its limit passes. Suppress one
finding with a next-line directive naming `type.nullable` or `type.union`.
Disabling `type.contract` turns off both diagnostics together.

**Options**: `enabled` (boolean, default `true`); `max_members` (integer, at
least 1, default 4) limits non-nullish members per union; `optional_fields`
(integer, at least 1, default 3) limits optional or nullable properties per
declaration; `null_defaults` (integer, at least 1, default 3) limits bare
nullish fallbacks per innermost function body.

Correct:

```json
{
  "checkers": {
    "type.contract": {
      "max_members": 6,
      "optional_fields": 4,
      "null_defaults": 5
    }
  }
}
```

Incorrect — the thresholds must be positive integers and `type.nullable` is not
a checker key:

```json
{
  "checkers": {
    "type.contract": {"max_members": 0},
    "type.nullable": {"enabled": false}
  }
}
```

This checker uses syntax only. It does not prove that every union should be
discriminated, that an optional property is unnecessary, or that a fallback is
semantically wrong; review the owning contract and its consumers. Broad-marker
and assertion resolution limits remain on their own pages.
