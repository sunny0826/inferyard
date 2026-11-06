# `type.broad`

[Back to the checker index](../checkers.md)

Reports recognized broad type markers and JSON serialization of internal values
outside a proven output or a permitted library input.

**Severity**: error.

**Languages**: every analyzer, with different markers:

| Language | Markers |
| --- | --- |
| Python | `typing.Any`, `typing_extensions.Any` and `pydantic.JsonValue` references, plus internal `json.dumps` results |
| Go | `any` and empty interfaces, plus imported `encoding/json` `Marshal`/`MarshalIndent` results |
| Java | `java.lang.Object`, unbounded wildcards and recognized raw collections, plus recognized Gson/Jackson results |
| JavaScript and JSX | `JSON.stringify` results only; JavaScript has no type annotations to check |
| TypeScript and TSX | `any`, `unknown`, `object` and empty object types, plus `JSON.stringify` results; a generic type argument such as the second `unknown` in `Record<string, unknown>` is matched at its own node |

An encoding is permitted when it flows exclusively to a proven output or to a
configured library input. Unshadowed `print`, standard streams, `pathlib`
`write_text` and writes to an enclosing writable text `open` context count as
outputs. A local binding is permitted only when it has one assignment and every
resolved read reaches permitted output; internal consumers, reassigned carriers
and `json.loads(json.dumps(payload))` round trips remain findings. Merely listing
the encoder's module in `third_party_modules` does not exempt its output.
Computed members, destructured aliases, runtime monkey-patching,
control-flow-sensitive reassignment and cross-function value flow are not proved.

#### Java

Java markers separate standard-library erasure from project names: recognized raw
standard collections need explicit import or qualified-name evidence, and locally
declared or explicitly imported custom `Object` and collection names are not
mistaken for standard-library erasure. Bounded wildcards retain their bound, and
parameterized containers are still checked for nested broad types. Raw collection
recognition is deliberately finite and does not infer arbitrary user-defined
generic declarations.

## Configuration

**Options**: `enabled` (boolean, default `true`); `encoding` (object, default all
four lists empty):

- `encoders`: exact qualified API identities whose result is serialized data.
- `decoders`: exact qualified reconstruction functions. Passing an encoding into
  one remains a finding, including through callable aliases or forwarded locals.
- `outputs`: declared output calls, each `{"call": ..., "arguments": [...],
  "keywords": [...]}`, where `arguments` holds zero-based nonnegative positional
  indexes and `keywords` holds Python keyword names. At least one selector is
  required; an empty selector list does not mean all arguments.
- `instance_outputs`: approved class-owned output members, each `{"path": ...,
  "class_name": ..., "member": ..., "arguments": [...]}`. The path is an exact,
  case-sensitive, normalized repository-relative POSIX path; class and member
  names are ASCII JavaScript identifiers; `arguments` is a non-empty list of
  zero-based nonnegative indexes.

API identities are non-empty strings without whitespace or the characters `*`,
`?`, `[` and `]`; names are exact and case-sensitive and use resolved import
identities rather than local spellings, so `from orjson import dumps as pack`
matches `orjson.dumps`. Boolean, string and fractional indexes are rejected.
These lists extend built-in recognition; they never remove built-in encoders,
decoders or output checks, and they never exempt `Any`, casts or incoming library
results.

Correct:

```json
{
  "checkers": {
    "type.broad": {
      "encoding": {
        "encoders": ["orjson.dumps"],
        "decoders": ["orjson.loads"],
        "outputs": [
          {"call": "transport.publish", "arguments": [1], "keywords": ["body"]}
        ]
      }
    }
  }
}
```

Incorrect — an output without a payload selector, a wildcard encoder identity,
and the superseded `type.erasure` key are all rejected:

```json
{
  "checkers": {
    "type.broad": {
      "encoding": {
        "encoders": ["orjson.*"],
        "outputs": [{"call": "transport.publish"}]
      }
    },
    "type.erasure": {"enabled": false}
  }
}
```
