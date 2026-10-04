# apprewrite

One-shot migration tool for narrowing `*App` parameters in
`internal/feishuapp`. **It is not part of the product build** — it lives in
its own module so `go build ./...` and `staticcheck ./...` skip it.

It exists because the migration is a fixpoint loop rather than a batch edit:
80 of the 92 functions that use exactly one `App` member also forward the
aggregate to callees that still take it, so each round only frees the layer
whose callees have already been converted.

## Usage

```
go run ./scripts/apprewrite <repo-root> < spec.json
```

`spec.json` is a list of:

```json
{ "func": "commandFast", "member": "bindings.ServiceTier",
  "new_type": "*servicetier.Service", "new_name": "servicetier",
  "import": "servicetier", "is_method": false, "accessor": "" }
```

For each entry the tool:

- replaces the `*App` parameter (matched through `go/ast`, at the position
  recorded for the named function, receiver-aware) with `<new_name> <new_type>`
- rewrites `a.Member` / `a.bindings.Member` in the body to `new_name`,
  dropping the call parentheses for no-arg method members
- appends `.Member` (or `.Accessor()`) to the matching argument at every call
  site, so callers passing the aggregate keep compiling

Call sites outside `internal/feishuapp` are only touched when they are
qualified as `feishuapp.F(...)`, and definitions are only rewritten inside
`internal/feishuapp`. Both restrictions exist because a same-named function in
an unrelated package was otherwise rewritten silently.

Companion helpers used alongside it live in the same commit series:
`scripts/apprewrite/` does not deduplicate imports, so a round that adds a
second alias for a package already imported needs the dedup pass applied
afterwards (see the round commits).
