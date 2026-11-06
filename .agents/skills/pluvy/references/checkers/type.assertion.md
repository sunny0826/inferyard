# `type.assertion`

[Back to the checker index](../checkers.md)

Reports unchecked type assertions and casts.

**Severity**: error.

**Languages**: Python, Java, TypeScript and TSX; Go and JavaScript do not
register this diagnostic.

Each language exposes a different set of forms:

| Language | Forms |
| --- | --- |
| TypeScript | `as` assertions except `as const`, non-null assertions, and angle-bracket assertions written `<T>value`, such as `<number>value`. |
| TSX | `as` assertions except `as const`, and non-null assertions. The angle-bracket form is JSX syntax in TSX files, so it is not recognized there and produces no `type.assertion` finding. |
| Java | reference casts such as `(String) value`; primitive numeric and boolean casts are permitted. |

#### Python

calls to `typing.cast` and `typing_extensions.cast`, and calls to
`typing.no_type_check`. The decorator form is classified separately: a
`@no_type_check` or `@no_type_check_decorator` marker produces
`type.suppression` and no `type.assertion` finding, because it is not a call.
A `typing.no_type_check(...)` call reports both `type.assertion` for the call
and `type.suppression` for the marker reference.

## Configuration

**Options**: None beyond `enabled` (boolean, default `true`).

Correct:

```json
{
  "checkers": {
    "type.assertion": {"enabled": false}
  }
}
```

Incorrect — unknown option:

```json
{
  "checkers": {
    "type.assertion": {"allow_as_const": false}
  }
}
```

`as const` is always allowed and needs no configuration.
