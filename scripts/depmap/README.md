# depmap

AST analysis tool for `*App` coupling in `internal/feishuapp`. Built for the
aggregate-removal migration (now complete; the coupling budget is pinned at
zero by `TestFeishuAppAggregateDoesNotGrow`), kept for future coupling audits.

It parses the package with `go/ast` and reports, for every function that takes
`*App`:

- `direct` — `a.X` members it touches
- `bindings` — `a.bindings.Y` service-locator fields it reaches
- `calls_app` — other `*App`-taking helpers it calls

From that, the transitive dependency set of each `*Ports` factory and the
fan-in of each helper are computed, which gives the construction order.
Do not recreate these numbers with regexes; the AST is the point.

```
go run ./scripts/depmap <repo>/internal/feishuapp
```
