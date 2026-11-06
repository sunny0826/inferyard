# `structure.guarded_projection`

[Back to the checker index](../checkers.md)

Reports repeated conditional copying of destination fields in one consecutive
local region. It is an error-level maintainability finding: it does not prove a
runtime defect, and it does not prove that a guard can safely be removed or
replaced by a spread.

**Severity**: error.

**Languages**: every analyzer. Region shape, recognized predicates and
projection syntax differ:

| Language | Regions | Predicates | Projection syntax |
| --- | --- | --- | --- |
| JavaScript, TypeScript, TSX | Blocks and the top-level program body | Built-in `undefined` with `===`/`!==`; nullish `null` with `==`/`!=`; strict equality to boolean, number or string literals | Guarded assignments; ternary or `&&` single-property object spreads |
| Python | Indented blocks and the module body | `None` with `is`/`is not` or `==`/`!=`; literal equality with `==`; boolean identity with `is` | Guarded attribute or literal-string-key assignments; conditional dictionary unpacking |
| Go | Statement lists | Unshadowed `nil` with `==`/`!=`; equality to unshadowed booleans, numbers or strings | Guarded selector assignments; no conditional spread form |
| Java | Method and constructor bodies | `null` with `==`/`!=`; equality to boolean, number, character or string literals | Guarded field assignments; no conditional spread form |

A candidate copies the tested source field or a primitive literal established by
equality, with an absent or empty opposite branch. Signed numeric literals are
supported. String recognition is conservative: plain quoted literals and Go raw
strings without escapes or newlines are recognized, while triple-quoted strings,
interpolation and escape decoding are not inferred. Go map indexing, Java
accessors/setters and collection calls are outside the field contract, and no
conditional spread form is claimed for Go or Java. Presence, nullishness and
literal equality remain distinct in diagnostic evidence. Truthiness, compound
predicates, arithmetic, conversion, validation, early exits and business
operations do not establish the pattern.

For absence and null checks the active branch can be inverted by leaving the
opposite branch empty; Python `pass` counts as empty. A cluster is a consecutive
sequence within one statement block, the Python module body, the JavaScript,
TypeScript and TSX top-level program body, or one object literal, sharing one
source receiver and one destination binding or construction. Comments and empty
statements are ignored. Other statements, ordinary properties, unrelated
spreads, different receivers or different destinations end the cluster, and
repeated writes to the same destination field count once. Nested functions,
loops, branches and nested objects have their own regions. No cluster crosses a
call, reassignment, control-flow join or separate literal, and a qualifying
cluster inside a loop or nested function is still reported.

In JavaScript, TypeScript and TSX, property renaming, parentheses, braces,
optional chaining and literal string bracket access preserve recognition.
Literal computed object keys are supported; dynamic computed keys are excluded.
JavaScript `__proto__: value` entries set a prototype instead of copying a
property, while computed `["__proto__"]: value` entries remain ordinary
properties. Python attribute access and item lookup retain different
identities, including nested receivers and destination fields; only one scalar
string subscript is supported, and tuple or multidimensional indexes are
excluded.

Receiver identity uses the existing lexical semantic index and static member
paths. JavaScript, TypeScript and TSX `const` aliases are followed only with a
preceding single initializer and no reassignment of the referenced binding;
mutable aliases are not equated with their sources. Python, Go and Java aliases
retain their own lexical identity. Property-initialized locals such as
`const usage = response.usage` retain their local binding instead of being
equated with repeated property reads. Unresolved bindings, dynamic property
paths, method-call receivers and alias cycles do not establish identity; this is
local evidence, not whole-program alias or getter analysis. JavaScript catch and
destructuring declarations belong to its existing binding adapter, so built-in
`undefined` checks respect those scopes.

One finding is anchored at the first guard or spread and records the distinct
field count, receiver, destination, participating `line:column` locations and
the predicates. The report schema is unchanged, complexity findings can coexist,
and existing changed-definition selection and staged snapshot isolation still
apply.

**Options**: `enabled` (boolean, default `true`); `min_fields` (integer, at least
2, default 3). The threshold is inclusive and counts distinct destination fields;
booleans, strings and fractional numbers are rejected.

Correct:

```json
{
  "checkers": {
    "structure.guarded_projection": {"min_fields": 4}
  }
}
```

Incorrect — an unknown option and a threshold below the minimum are both
rejected:

```json
{
  "checkers": {
    "structure.guarded_projection": {"minimum_fields": 4, "min_fields": 1}
  }
}
```
