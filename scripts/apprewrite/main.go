// apprewrite narrows a *App parameter to the single capability the function
// actually uses: the parameter becomes that capability's value, member
// accesses in the body become the parameter, and call sites gain ".member".
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Spec struct {
	Func     string `json:"func"`      // function name
	Member   string `json:"member"`    // App member it uses
	NewType  string `json:"new_type"`  // replacement parameter type
	NewName  string `json:"new_name"`  // replacement parameter name
	Import   string `json:"import"`    // qualifier whose import the new type needs
	IsMethod bool   `json:"is_method"` // App member is a method (call sites need "()")
	Accessor string `json:"accessor"`  // exported accessor for cross-package call sites
}

// importLines maps a qualifier to the import line to add when a rewritten file
// needs it.
var importLines = map[string]string{}

type edit struct {
	start, end int
	text       string
}

var (
	fset  = token.NewFileSet()
	cache = map[string]string{}
)

func src(p string) string {
	if s, ok := cache[p]; ok {
		return s
	}
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	cache[p] = string(b)
	return cache[p]
}

func main() {
	root := os.Args[1]
	var specs []Spec
	if err := json.NewDecoder(os.Stdin).Decode(&specs); err != nil {
		panic(err)
	}
	byName := map[string]Spec{}
	for _, s := range specs {
		byName[s.Func] = s
	}

	// learn alias -> import line from the package's own files
	for _, p := range mustGlob(filepath.Join(root, "internal/feishuapp/*.go")) {
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			continue
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			alias := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			line := "\t"
			if imp.Name != nil {
				line += imp.Name.Name + " "
			}
			line += `"` + path + `"`
			if _, ok := importLines[alias]; !ok {
				importLines[alias] = line
			}
			aliasPaths[alias] = path
		}
	}

	var files []string
	filepath.Walk(filepath.Join(root, "internal"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)

	paramIdx := map[string]int{}
	defFile := map[string]string{}
	recvOf := map[string]string{} // "" for free functions
	for _, p := range files {
		if !strings.Contains(p, "/internal/feishuapp/") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			panic(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type.Params == nil {
				continue
			}
			if _, want := byName[fd.Name.Name]; !want {
				continue
			}
			recv := ""
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				recv = recvTypeName(fd.Recv.List[0].Type)
			}
			for i, prm := range fd.Type.Params.List {
				if star, ok := prm.Type.(*ast.StarExpr); ok {
					if id, ok := star.X.(*ast.Ident); ok && id.Name == "App" {
						paramIdx[fd.Name.Name] = i
						defFile[fd.Name.Name] = p
						recvOf[fd.Name.Name] = recv
					}
				}
			}
		}
	}
	for _, s := range specs {
		if _, ok := paramIdx[s.Func]; !ok {
			fmt.Fprintf(os.Stderr, "  NOT FOUND: %s\n", s.Func)
		}
	}

	edits := map[string][]edit{}
	for _, p := range files {
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			panic(err)
		}
		text := src(p)
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Body == nil || x.Type.Params == nil {
					return true
				}
				if !strings.Contains(p, "/internal/feishuapp/") {
					return true
				}
				sp, want := byName[x.Name.Name]
				if !want {
					return true
				}
				recv := ""
				if x.Recv != nil && len(x.Recv.List) > 0 {
					recv = recvTypeName(x.Recv.List[0].Type)
				}
				if recv != recvOf[x.Name.Name] {
					return true
				}
				idx, ok := paramIdx[x.Name.Name]
				if !ok || idx >= len(x.Type.Params.List) {
					return true
				}
				prm := x.Type.Params.List[idx]
				if len(prm.Names) == 0 {
					return true
				}
				varName := prm.Names[0].Name
				// replace the parameter
				edits[p] = append(edits[p], edit{
					fset.Position(prm.Pos()).Offset, fset.Position(prm.End()).Offset,
					sp.NewName + " " + sp.NewType,
				})
				// rewrite member accesses in the body
				// no-arg method calls: a.M() -> the value; record the call to strip "()"
				stripCall := map[ast.Node]bool{}
				ast.Inspect(x.Body, func(m ast.Node) bool {
					call, ok := m.(*ast.CallExpr)
					if !ok || len(call.Args) != 0 {
						return true
					}
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == sp.Member {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == varName {
							stripCall[sel] = true
						}
					}
					// a.bindings.M() -> value: record the outer selector
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(sp.Member, "bindings.") {
						if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "bindings" {
							if id, ok := inner.X.(*ast.Ident); ok && id.Name == varName && sel.Sel.Name == strings.TrimPrefix(sp.Member, "bindings.") {
								stripCall[sel] = true
							}
						}
					}
					return true
				})
				// a.M  (single level) or a.bindings.M (two levels)
				wantDirect := sp.Member
				wantBinding := ""
				if strings.HasPrefix(sp.Member, "bindings.") {
					wantDirect = ""
					wantBinding = strings.TrimPrefix(sp.Member, "bindings.")
				}
				ast.Inspect(x.Body, func(m ast.Node) bool {
					sel, ok := m.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					match := false
					if wantDirect != "" {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == varName && sel.Sel.Name == wantDirect {
							match = true
						}
					}
					if wantBinding != "" && sel.Sel.Name == wantBinding {
						if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "bindings" {
							if id, ok := inner.X.(*ast.Ident); ok && id.Name == varName {
								match = true
							}
						}
					}
					if !match {
						return true
					}
					end := fset.Position(sel.End()).Offset
					if stripCall[sel] && end+2 <= len(src(p)) && src(p)[end:end+2] == "()" {
						end += 2
					}
					edits[p] = append(edits[p], edit{fset.Position(sel.Pos()).Offset, end, sp.NewName})
					return true
				})
			case *ast.CallExpr:
				var fname string
				qualified := false
				switch fn := x.Fun.(type) {
				case *ast.Ident:
					fname = fn.Name
				case *ast.SelectorExpr:
					fname = fn.Sel.Name
					qualified = true
				default:
					return true
				}
				sp, want := byName[fname]
				if !want {
					return true
				}
				// Only rewrite calls that can actually be passing an App: calls
				// inside feishuapp, or qualified feishuapp.F(...) calls from
				// elsewhere. Name collisions in unrelated packages otherwise
				// get rewritten too.
				idx, ok := paramIdx[fname]
				if !ok || idx >= len(x.Args) || x.Ellipsis.IsValid() {
					return true
				}
				inPkg := strings.Contains(p, "/internal/feishuapp/")
				if !inPkg {
					if !qualified || !strings.HasSuffix(qualifierOf(x.Fun), "/feishuapp") {
						return true
					}
					if _, isIdent := x.Args[idx].(*ast.Ident); !isIdent {
						return true
					}
				}
				arg := x.Args[idx]
				access := "." + sp.Member
				if qualified {
					if sp.Accessor == "" {
						return true
					} // internal-only member
					access = "." + sp.Accessor
				}
				if sp.IsMethod || qualified {
					access += "()"
				}
				edits[p] = append(edits[p], edit{
					fset.Position(arg.End()).Offset, fset.Position(arg.End()).Offset, access,
				})
			}
			return true
		})
		_ = text
	}
	// Apply text edits first, then fix imports by re-parsing the edited files.
	needByFile := map[string]map[string]bool{}
	for _, sp := range specs {
		if sp.Import == "" {
			continue
		}
		f, ok := defFile[sp.Func]
		if !ok {
			continue
		}
		if needByFile[f] == nil {
			needByFile[f] = map[string]bool{}
		}
		needByFile[f][sp.Import] = true
	}
	n := 0
	for p, es := range edits {
		apply(p, es)
		n++
	}
	for p, quals := range needByFile {
		addMissingImports(p, quals)
	}
	fmt.Fprintf(os.Stderr, "rewrote %d files\n", n)
}

func apply(p string, es []edit) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].start != es[j].start {
			return es[i].start > es[j].start
		}
		return es[i].end > es[j].end
	})
	out := src(p)
	for _, e := range es {
		out = out[:e.start] + e.text + out[e.end:]
	}
	os.WriteFile(p, []byte(out), 0o644)
	delete(cache, p)
}

func mustGlob(pattern string) []string { m, _ := filepath.Glob(pattern); return m }

func recvTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// qualifierOf returns the import path a package qualifier resolves to, using
// the alias map learned from the package's imports.
func qualifierOf(fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return aliasPaths[id.Name]
}

var aliasPaths = map[string]string{}

// addMissingImports re-parses the file and inserts any needed import whose
// qualifier is not already present.
func addMissingImports(path string, quals map[string]bool) {
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		panic(err)
	}
	present := map[string]bool{}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		a := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			a = imp.Name.Name
		}
		present[a] = true
	}
	var add []string
	for q := range quals {
		if present[q] {
			continue
		}
		line, ok := importLines[q]
		if !ok {
			fmt.Fprintf(os.Stderr, "  no import line for %q (needed by %s)\n", q, filepath.Base(path))
			continue
		}
		add = append(add, line)
	}
	if len(add) == 0 {
		return
	}
	sort.Strings(add)
	srcTxt := src(path)
	i := strings.Index(srcTxt, "import (\n")
	if i < 0 {
		panic("no import block in " + path)
	}
	ins := strings.Join(add, "\n") + "\n"
	out := srcTxt[:i+len("import (\n")] + ins + srcTxt[i+len("import (\n"):]
	os.WriteFile(path, []byte(out), 0o644)
	delete(cache, path)
}
