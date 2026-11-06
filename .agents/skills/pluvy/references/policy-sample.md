# Policy sample — compact starting point

A repository commits its policy at `.pluvy/pluvy.json`, and the gate
never changes it. The policy keeps global file selection separate from checker
options. The document below mirrors
`examples/.pluvy/pluvy.json` verbatim, covering the settings a
new repository decides first: which files are selected, which function-size
thresholds apply, and which ownership and import boundaries hold.

```json
{
  "version": 2,
  "include": ["src/*", "tests/*"],
  "exclude": [],
  "checkers": {
    "type.broad": {
      "encoding": {
        "encoders": ["orjson.dumps"],
        "decoders": ["orjson.loads"],
        "outputs": [
          {"call": "transport.publish", "arguments": [1], "keywords": ["body"]}
        ]
      }
    },
    "function_size": {
      "lines": 50,
      "statements": 50,
      "complexity": 10,
      "nesting": 3,
      "parameters": 5
    },
    "architecture.owner": {
      "owners": [
        {"name": "domain", "paths": ["src/domain/*"]},
        {"name": "application", "paths": ["src/application/*"]},
        {"name": "adapters", "paths": ["src/adapters/*"]},
        {"name": "tests", "paths": ["tests/*"]}
      ]
    },
    "architecture.import": {
      "boundaries": [
        {
          "name": "domain does not depend on adapters",
          "sources": ["src/domain/*"],
          "forbidden_imports": [
            "adapters",
            "adapters.*",
            "src.adapters",
            "src.adapters.*",
            "src/adapters/*"
          ]
        }
      ]
    },
    "architecture.object_model": {"paths": ["src/domain/rules/*"]}
  }
}
```

Every entry below is a policy decision that a threshold, exclude, owner or
boundary change reopens for a separate full audit and review:

- `include` and `exclude` are repository-relative glob patterns over the audited
  tree. An empty `exclude` list is valid; a pattern silently removed here changes
  what the gate reads.
- `checkers["architecture.owner"].owners` gives each name a set of paths, and a
  selected file must match exactly one owner. Keep the owner set small and
  named for the responsibility it holds.
- `checkers["architecture.import"].boundaries` names one forbidden dependency
  direction: `sources` match the importing files and `forbidden_imports` lists
  both module prefixes and path forms of the dependency.
- `checkers["function_size"]` holds the five readability thresholds — physical
  lines, statements, complexity, control nesting and parameters — whose defaults
  are 50, 50, 10, 3 and 5.
- `checkers["architecture.object_model"].paths` selects the extension
  implementation paths covered by the object-model rule.
- `checkers["type.broad"].encoding` extends built-in recognition with exact
  encoder and decoder API identities plus declared output calls; it never exempts
  broad annotations or incoming library results.
