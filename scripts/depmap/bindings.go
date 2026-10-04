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

// Stmt is one `bindings.X = ...` assignment in composition.
type Stmt struct {
	Produces string   `json:"produces"`
	Line     int      `json:"line"`
	Funcs    []string `json:"funcs"`  // feishuapp.* functions called
	Direct   []Read   `json:"direct"` // bindings.X read in the statement itself
}

type BindingGraph struct {
	Funcs FuncReads           `json:"func_reads"`
	Stmts []Stmt              `json:"stmts"`
	Eager map[string][]string `json:"eager_edges"` // producer -> producers it needs at construction time
	Any   map[string][]string `json:"any_edges"`   // including lazy reads
}

func scanBindings(repoRoot string) *BindingGraph {
	g := &BindingGraph{Funcs: FuncReads{}, Eager: map[string][]string{}, Any: map[string][]string{}}
	fs := token.NewFileSet()

	// pass 1: what every feishuapp function reads
	dir := filepath.Join(repoRoot, "internal/feishuapp")
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fs, p, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			var appVar string
			if fd.Type.Params != nil {
				for _, prm := range fd.Type.Params.List {
					if star, ok := prm.Type.(*ast.StarExpr); ok {
						if id, ok := star.X.(*ast.Ident); ok && id.Name == "App" && len(prm.Names) > 0 {
							appVar = prm.Names[0].Name
						}
					}
				}
			}
			if appVar == "" {
				continue
			}
			g.Funcs[fd.Name.Name] = collectReads(fd.Body, appVar)
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

	// resolve edges
	assigned := map[string]bool{}
	for _, st := range g.Stmts {
		assigned[st.Produces] = true
	}
	for _, st := range g.Stmts {
		eager := map[string]bool{}
		any := map[string]bool{}
		for _, r := range st.Direct {
			if r.Binding == st.Produces || !assigned[r.Binding] {
				continue
			}
			any[r.Binding] = true
			if !r.Lazy {
				eager[r.Binding] = true
			}
		}
		for _, fn := range st.Funcs {
			for _, r := range g.Funcs[fn] {
				if r.Binding == st.Produces || !assigned[r.Binding] {
					continue
				}
				any[r.Binding] = true
				if !r.Lazy {
					eager[r.Binding] = true
				}
			}
		}
		g.Eager[st.Produces] = keysOf(eager)
		g.Any[st.Produces] = keysOf(any)
	}
	return g
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
