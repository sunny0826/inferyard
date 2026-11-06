# `type.annotation`

[Back to the checker index](../checkers.md)

Reports broad or bare Python annotations and invalid forward annotations.

**Severity**: error.

**Languages**: Python only.

Broad markers are `Any`, `object`, `JsonValue` and `BaseModel`. Bare built-in and
typing generics are reported, including `list`, `dict`, `set`, `tuple`,
`frozenset`, `List`, `Dict`, `Set`, `Tuple`, `Mapping`, `MutableMapping`,
`Sequence`, `Iterable`, `Iterator` and `Callable` without parameters. `Callable`
with an ellipsis parameter list is reported as well. `Literal[...]` contents are
not flagged, and `Annotated[T, ...]` checks only its payload type `T`. A forward
annotation given as a string is reported when it does not parse as a single valid
annotation expression. A `typing.Any` reference also reports `type.broad`, so one
annotation can produce a finding from each checker.

**Options**: None beyond `enabled` (boolean, default `true`).

Correct — the checker keeps its defaults and no option is needed:

```json
{
  "checkers": {}
}
```

Incorrect — `type.annotation` has no additional options:

```json
{
  "checkers": {
    "type.annotation": {"strict": true}
  }
}
```
