# depmap

Analysis tool for the `*App` aggregate removal in `internal/feishuapp`
(see [docs/feishuapp-app-aggregate-removal.md](../../docs/feishuapp-app-aggregate-removal.md)).

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
