# `type.boundary`

[Back to the checker index](../checkers.md)

Reports recognized third-party results and callback inputs that reach
application code without conversion into a declared typed object.

**Severity**: error.

**Languages**: every analyzer.

Library inputs may use an erased dictionary: a dictionary, or a directly forwarded
dictionary parameter, is permitted when every read supplies an argument to a
configured library. Python `**kwargs`, Go maps and TypeScript `Record` annotations
are recognized. A callback registered as a library argument may return that
dictionary contract, but its incoming dictionary parameters must be converted at
callback ingress. Objects passed to a configured library have all their declared
fields treated as consumed. Type assertions and suppression directives remain
errors, and mechanical recognition of a conversion does not prove the validator
is honest. These are bounded semantic checks, not compiler or whole-program
proofs. Dynamic imports, arbitrary aliases, reflection, dynamic attributes,
complex generic flow, framework registration and external consumers may require
review.

#### Java

Java does not currently provide the dictionary-carrier exceptions implemented for
Python and JavaScript/TypeScript. Java imports are configured by exact class or
static-member identity: `import vendor.Api` matches
`"third_party_modules": ["vendor.Api"]`, and a static import of `vendor.Api.fetch`
uses that full member name. Architecture patterns such as `vendor.*` match both.
Import wildcards are dependency evidence and are not expanded into semantic
bindings.

## Configuration

**Options**: None beyond `enabled` (boolean, default `true`). Classification comes
from the top-level `third_party_modules` list of exact import specifiers; import
aliases retain that identity. An empty list is the default. This setting grants a
directional boundary exception, not a file or checker exemption, and disabling
`type.boundary` does not change the third-party classification that other type
and field checks use.

Correct:

```json
{
  "version": 2,
  "third_party_modules": ["vendor.client", "@vendor/sdk", "example.com/vendor/sdk"],
  "checkers": {}
}
```

Incorrect — `third_party_modules` is global, not a checker option:

```json
{
  "version": 2,
  "checkers": {
    "type.boundary": {"third_party_modules": ["vendor.client"]}
  }
}
```
