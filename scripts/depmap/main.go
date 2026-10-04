// depmap builds the dependency blueprint for removing feishuapp's *App
// aggregate: for every *Ports factory, which *App-taking helpers it calls
// (transitively through closures) and which aggregate members it touches.
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

type FuncInfo struct {
	Name      string   `json:"name"`
	File      string   `json:"file"`
	TakesApp  bool     `json:"takes_app"`
	Direct    []string `json:"direct"`
	Bindings  []string `json:"bindings"`
	CallsApp  []string `json:"calls_app"` // *App-taking helpers this function calls
	IsFactory bool     `json:"is_factory"`
}

func main() {
	dir := os.Args[1]
	fset := token.NewFileSet()
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))

	type parsed struct {
		path string
		file *ast.File
	}
	var ps []parsed
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ps = append(ps, parsed{p, f})
	}

	// pass 1: which functions take *App, and what is their App param called
	appParam := map[string]string{}
	info := map[string]*FuncInfo{}
	var order []string
	for _, pr := range ps {
		for _, d := range pr.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type.Params == nil {
				continue
			}
			for _, prm := range fd.Type.Params.List {
				star, ok := prm.Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				id, ok := star.X.(*ast.Ident)
				if !ok || id.Name != "App" || len(prm.Names) == 0 {
					continue
				}
				appParam[fd.Name.Name] = prm.Names[0].Name
				fi := &FuncInfo{Name: fd.Name.Name, File: pr.path,
					TakesApp: true, IsFactory: strings.HasSuffix(fd.Name.Name, "Ports")}
				info[fd.Name.Name] = fi
				order = append(order, fd.Name.Name)
			}
		}
	}

	// pass 2: for each such function, what does it touch and call
	for _, pr := range ps {
		for _, d := range pr.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fi, ok := info[fd.Name.Name]
			if !ok {
				continue
			}
			varName := appParam[fd.Name.Name]
			direct := map[string]bool{}
			bindings := map[string]bool{}
			calls := map[string]bool{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					base, ok := x.X.(*ast.Ident)
					if !ok || base.Name != varName {
						// a.bindings.Y has the selector nested one level deeper
						if inner, ok := x.X.(*ast.SelectorExpr); ok {
							if id, ok := inner.X.(*ast.Ident); ok && id.Name == varName && inner.Sel.Name == "bindings" {
								bindings[x.Sel.Name] = true
							}
						}
						return true
					}
					if x.Sel.Name != "bindings" {
						direct[x.Sel.Name] = true
					}
				case *ast.CallExpr:
					fname := ""
					switch fn := x.Fun.(type) {
					case *ast.Ident:
						fname = fn.Name
					case *ast.SelectorExpr:
						// pkg.F(...) is not a package-local helper
					}
					if _, ok := appParam[fname]; ok && fname != fd.Name.Name {
						calls[fname] = true
					}
				}
				return true
			})
			for k := range direct {
				fi.Direct = append(fi.Direct, k)
			}
			for k := range bindings {
				fi.Bindings = append(fi.Bindings, k)
			}
			for k := range calls {
				fi.CallsApp = append(fi.CallsApp, k)
			}
			sort.Strings(fi.Direct)
			sort.Strings(fi.Bindings)
			sort.Strings(fi.CallsApp)
		}
	}

	var out []*FuncInfo
	for _, n := range order {
		out = append(out, info[n])
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	enc.Encode(out)
	fmt.Fprintf(os.Stderr, "mapped %d *App-taking functions\n", len(out))
}
