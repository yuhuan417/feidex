package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Read is one binding access found in a function body or a composition
// statement. Lazy means it sits inside a func literal, so it happens when the
// closure runs rather than when the factory is called.
type Read struct {
	Binding string `json:"binding"`
	Lazy    bool   `json:"lazy"`
}

// FuncReads maps a feishuapp function to every binding it reads.
type FuncReads map[string][]Read

// Call is an edge from one function to another *App-taking function or to a
// struct whose methods hold an *App field. Lazy means the call sits inside a
// func literal, so the callee runs when the closure runs.
type Call struct {
	Fn   string `json:"fn"`
	Lazy bool   `json:"lazy"`
}

// Stmt is one `bindings.X = ...` assignment in composition.
type Stmt struct {
	Produces string   `json:"produces"`
	Line     int      `json:"line"`
	Funcs    []string `json:"funcs"`  // feishuapp.* functions called
	Direct   []Read   `json:"direct"` // bindings.X read in the statement itself
}

type BindingGraph struct {
	Funcs FuncReads           `json:"func_reads"`
	Calls map[string][]Call   `json:"calls"` // function -> *App-taking callees and instantiated structs
	Stmts []Stmt              `json:"stmts"`
	Eager map[string][]string `json:"eager_edges"` // producer -> producers it needs at construction time
	Any   map[string][]string `json:"any_edges"`   // including lazy reads
}

func scanBindings(repoRoot string) *BindingGraph {
	g := &BindingGraph{Funcs: FuncReads{}, Calls: map[string][]Call{}, Eager: map[string][]string{}, Any: map[string][]string{}}
	fs := token.NewFileSet()

	dir := filepath.Join(repoRoot, "internal/feishuapp")
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))

	// pass 0: parse everything, and learn which names take *App (called
	// functions) and which structs hold an *App field (instantiated structs).
	var parsed []*ast.File
	appFuncs := map[string]bool{}
	structAppField := map[string]string{}
	var decls []*ast.FuncDecl
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fs, p, nil, 0)
		if err != nil {
			continue
		}
		parsed = append(parsed, f)
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				decls = append(decls, decl)
				if decl.Recv == nil && takesAppParam(decl) != "" {
					appFuncs[decl.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, fld := range st.Fields.List {
						star, ok := fld.Type.(*ast.StarExpr)
						if !ok || len(fld.Names) == 0 {
							continue
						}
						if id, ok := star.X.(*ast.Ident); ok && id.Name == "App" {
							structAppField[ts.Name.Name] = fld.Names[0].Name
						}
					}
				}
			}
		}
	}

	// pass 1: what every function and *App-holding struct method reads, and
	// which *App-taking functions or App-holding structs it reaches.
	structMethods := map[string][]string{}
	for _, fd := range decls {
		if fd.Body == nil {
			continue
		}
		key, appVar := "", ""
		if fd.Recv == nil {
			appVar = takesAppParam(fd)
			if appVar == "" {
				continue
			}
			key = fd.Name.Name
		} else {
			recv := recvTypeName(fd.Recv.List[0].Type)
			field, ok := structAppField[recv]
			if !ok {
				continue
			}
			appVar = field
			key = recv + "." + fd.Name.Name
		}
		g.Funcs[key] = collectReads(fd.Body, appVar)
		g.Calls[key] = collectCalls(fd.Body, appVar, appFuncs, structAppField)
		if fd.Recv != nil {
			recv := recvTypeName(fd.Recv.List[0].Type)
			structMethods[recv] = append(structMethods[recv], recv+"."+fd.Name.Name)
		}
	}

	// pass 2: composition statements
	compPath := filepath.Join(repoRoot, "internal/composition/app.go")
	src, err := os.ReadFile(compPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	text := string(src)
	lines := strings.Split(text, "\n")
	reStmt := regexp.MustCompile(`^\s*\*?bindings\.(\w+)\s*=`)
	i := 0
	for i < len(lines) {
		m := reStmt.FindStringSubmatch(lines[i])
		if m == nil {
			i++
			continue
		}
		depth := strings.Count(lines[i], "(") - strings.Count(lines[i], ")")
		j := i
		for depth > 0 && j+1 < len(lines) {
			j++
			depth += strings.Count(lines[j], "(") - strings.Count(lines[j], ")")
		}
		stmtText := strings.Join(lines[i:j+1], "\n")
		st := Stmt{Produces: m[1], Line: i + 1}
		for _, fm := range regexp.MustCompile(`feishuapp\.(\w+)\(`).FindAllStringSubmatch(stmtText, -1) {
			st.Funcs = append(st.Funcs, fm[1])
		}
		sort.Strings(st.Funcs)
		st.Direct = directReads(stmtText)
		g.Stmts = append(g.Stmts, st)
		i = j + 1
	}

	// Bindings pre-created as placeholders in the composite literal
	// (`Plan: &planapp.Service{}`) exist from the first line, so passing the
	// pointer around is not a construction-order constraint. Reads of them are
	// satisfied immediately.
	placeholder := map[string]bool{}
	if lm := regexp.MustCompile(`bindings\s*:?=\s*&?\w*\.?Bindings\{([\s\S]*?)\n\t\}`).FindStringSubmatch(text); lm != nil {
		for _, pm := range regexp.MustCompile(`(\w+):\s*&`).FindAllStringSubmatch(lm[1], -1) {
			placeholder[pm[1]] = true
		}
	}
	fmt.Fprintf(os.Stderr, "占位符 binding: %d 个\n", len(placeholder))

	// resolve edges
	assigned := map[string]bool{}
	for _, st := range g.Stmts {
		assigned[st.Produces] = true
	}
	for _, st := range g.Stmts {
		eager := map[string]bool{}
		any := map[string]bool{}
		for _, r := range st.Direct {
			if r.Binding == st.Produces || !assigned[r.Binding] || placeholder[r.Binding] {
				continue
			}
			any[r.Binding] = true
			if !r.Lazy {
				eager[r.Binding] = true
			}
		}
		for _, fn := range st.Funcs {
			eagerReads := g.reach(fn, true, structMethods, map[string]bool{})
			anyReads := g.reach(fn, false, structMethods, map[string]bool{})
			for b := range anyReads {
				if b == st.Produces || !assigned[b] || placeholder[b] {
					continue
				}
				any[b] = true
				if eagerReads[b] {
					eager[b] = true
				}
			}
		}
		g.Eager[st.Produces] = keysOf(eager)
		g.Any[st.Produces] = keysOf(any)
	}
	return g
}

// reach returns every binding a call to name reads. eagerOnly follows just the
// calls that happen when name runs; otherwise calls inside func literals are
// followed too and their reads count as lazy. Struct values are expanded into
// their methods: handing a struct out hands its *App field to every method.
//
// The result depends only on name and eagerOnly, so callers memoize it.
func (g *BindingGraph) reach(name string, eagerOnly bool, structMethods map[string][]string, visiting map[string]bool) map[string]bool {
	if visiting[name] {
		return nil
	}
	visiting[name] = true
	out := map[string]bool{}
	for _, r := range g.Funcs[name] {
		if eagerOnly && r.Lazy {
			continue
		}
		out[r.Binding] = true
	}
	for _, c := range g.Calls[name] {
		if eagerOnly && c.Lazy {
			continue
		}
		// A struct value handed out by the factory only runs its methods after
		// construction, so its reads are dependencies but never construction
		// order constraints.
		if methods, ok := structMethods[c.Fn]; ok {
			if eagerOnly {
				continue
			}
			for _, target := range methods {
				for b := range g.reach(target, false, structMethods, visiting) {
					out[b] = true
				}
			}
			continue
		}
		for b := range g.reach(c.Fn, eagerOnly, structMethods, visiting) {
			out[b] = true
		}
	}
	delete(visiting, name)
	return out
}

// takesAppParam returns the parameter name of a leading *App parameter, or "".
func takesAppParam(fd *ast.FuncDecl) string {
	if fd.Type.Params == nil {
		return ""
	}
	for _, prm := range fd.Type.Params.List {
		star, ok := prm.Type.(*ast.StarExpr)
		if !ok || len(prm.Names) == 0 {
			continue
		}
		if id, ok := star.X.(*ast.Ident); ok && id.Name == "App" {
			return prm.Names[0].Name
		}
	}
	return ""
}

func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// collectCalls records every *App-taking function called in the body and every
// App-holding struct literal it builds, marking calls inside func literals as
// lazy.
func collectCalls(body *ast.BlockStmt, appVar string, appFuncs map[string]bool, structAppField map[string]string) []Call {
	seen := map[string]bool{}
	var out []Call
	var walk func(n ast.Node, lazy bool)
	walk = func(n ast.Node, lazy bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.FuncLit:
				walk(x.Body, true)
				return false
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && appFuncs[id.Name] {
					key := fmt.Sprintf("%s/%v", id.Name, lazy)
					if !seen[key] {
						seen[key] = true
						out = append(out, Call{Fn: id.Name, Lazy: lazy})
					}
				}
			case *ast.CompositeLit:
				id, ok := x.Type.(*ast.Ident)
				if !ok {
					return true
				}
				if _, ok := structAppField[id.Name]; !ok {
					return true
				}
				key := fmt.Sprintf("%s/%v", id.Name, lazy)
				if !seen[key] {
					seen[key] = true
					out = append(out, Call{Fn: id.Name, Lazy: lazy})
				}
			}
			return true
		})
	}
	walk(body, false)
	sort.Slice(out, func(a, b int) bool {
		if out[a].Fn != out[b].Fn {
			return out[a].Fn < out[b].Fn
		}
		return !out[a].Lazy
	})
	return out
}

// collectReads walks a body and records every appVar.bindings.X read, marking
// reads inside func literals as lazy.
func collectReads(body *ast.BlockStmt, appVar string) []Read {
	seen := map[string]bool{}
	var out []Read
	var walk func(n ast.Node, lazy bool)
	walk = func(n ast.Node, lazy bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.FuncLit:
				walk(x.Body, true)
				return false
			case *ast.SelectorExpr:
				inner, ok := x.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := inner.X.(*ast.Ident)
				if !ok || id.Name != appVar || inner.Sel.Name != "bindings" {
					return true
				}
				key := fmt.Sprintf("%s/%v", x.Sel.Name, lazy)
				if !seen[key] {
					seen[key] = true
					out = append(out, Read{Binding: x.Sel.Name, Lazy: lazy})
				}
			}
			return true
		})
	}
	walk(body, false)
	sort.Slice(out, func(a, b int) bool {
		if out[a].Binding != out[b].Binding {
			return out[a].Binding < out[b].Binding
		}
		return !out[a].Lazy
	})
	return out
}

// directReads finds bindings.X reads in a composition statement.
func directReads(stmt string) []Read {
	fset := token.NewFileSet()
	wrap := "package p\nfunc _() {\n" + stmt + "\n}\n"
	f, err := parser.ParseFile(fset, "stmt.go", wrap, 0)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Read
	var walk func(n ast.Node, lazy bool)
	walk = func(n ast.Node, lazy bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.FuncLit:
				walk(x.Body, true)
				return false
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok || id.Name != "bindings" {
					return true
				}
				key := fmt.Sprintf("%s/%v", x.Sel.Name, lazy)
				if !seen[key] {
					seen[key] = true
					out = append(out, Read{Binding: x.Sel.Name, Lazy: lazy})
				}
			}
			return true
		})
	}
	walk(f, false)
	return out
}

func keysOf(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sccs returns every strongly connected component with more than one member.
func sccs(edges map[string][]string) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	counter := 0
	var strongconnect func(v string)
	strongconnect = func(v string) {
		index[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if _, ok := index[w]; !ok {
				strongconnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Strings(comp)
				out = append(out, comp)
			}
		}
	}
	for v := range edges {
		if _, ok := index[v]; !ok {
			strongconnect(v)
		}
	}
	return out
}

func runBindings(repoRoot string, asJSON bool) {
	g := scanBindings(repoRoot)
	eager := sccs(g.Eager)
	anyCycles := sccs(g.Any)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", " ")
		enc.Encode(map[string]any{"graph": g, "eager_cycles": eager, "any_cycles": anyCycles})
		return
	}

	fmt.Printf("composition 赋值语句: %d\n", len(g.Stmts))
	fmt.Printf("feishuapp 函数（收 *App）: %d\n\n", len(g.Funcs))

	fmt.Printf("=== 构造期环（只算 eager 读取）: %d 个 ===\n", len(eager))
	for _, c := range eager {
		fmt.Printf("    %s\n", strings.Join(c, " ↔ "))
	}
	fmt.Printf("\n=== 含惰性读取的环: %d 个 ===\n", len(anyCycles))
	for _, c := range anyCycles {
		fmt.Printf("    %s\n", strings.Join(c, " ↔ "))
	}

	// An eager read of a binding that is assigned later captures its zero
	// value. This is the check that catches reorderings which look topological
	// but are not, which is how WorkspaceConfiguration once captured a zero
	// BackendConfiguration.
	line := map[string]int{}
	for _, st := range g.Stmts {
		line[st.Produces] = st.Line
	}
	fmt.Printf("\n=== 反向 eager 读取（读了还没赋值的 binding）===\n")
	backward := 0
	for _, st := range g.Stmts {
		for _, dep := range g.Eager[st.Produces] {
			if assignedLine, ok := line[dep]; ok && assignedLine > st.Line {
				fmt.Printf("    %s (line %d) reads %s (assigned line %d)\n", st.Produces, st.Line, dep, assignedLine)
				backward++
			}
		}
	}
	if backward == 0 {
		fmt.Printf("    0 个\n")
	}

	// 每个环的边，标出 eager/lazy
	fmt.Printf("\n=== 含惰性读取的环，逐边 ===\n")
	for _, c := range anyCycles {
		inCycle := map[string]bool{}
		for _, n := range c {
			inCycle[n] = true
		}
		fmt.Printf("  [%s]\n", strings.Join(c, ", "))
		for _, n := range c {
			var eagerDeps, lazyDeps []string
			for _, d := range g.Eager[n] {
				if inCycle[d] {
					eagerDeps = append(eagerDeps, d)
				}
			}
			lazyOnly := map[string]bool{}
			for _, d := range g.Any[n] {
				if inCycle[d] {
					lazyOnly[d] = true
				}
			}
			for _, d := range eagerDeps {
				delete(lazyOnly, d)
			}
			for d := range lazyOnly {
				lazyDeps = append(lazyDeps, d)
			}
			sort.Strings(lazyDeps)
			if len(eagerDeps) > 0 || len(lazyDeps) > 0 {
				fmt.Printf("      %-24s eager→%v  lazy→%v\n", n, eagerDeps, lazyDeps)
			}
		}
	}
}
