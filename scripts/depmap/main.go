// depmap builds the dependency blueprint for removing feishuapp's *App
// aggregate.
//
// The earlier version only followed direct calls, which made a factory look
// like it had zero dependencies when its real dependencies lived in the
// methods of the structs it hands out. This version follows three edges:
//
//	factory --calls--> *App-taking helper
//	factory --instantiates--> struct type whose field is *App
//	struct method --calls--> *App-taking helper
//
// and closes transitively over all three.
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
	Name         string   `json:"name"`
	File         string   `json:"file"`
	TakesApp     bool     `json:"takes_app"`
	IsFactory    bool     `json:"is_factory"`
	Direct       []string `json:"direct"`
	Bindings     []string `json:"bindings"`
	CallsApp     []string `json:"calls_app"`
	Instantiates []string `json:"instantiates"` // App-holding struct types it builds
}

type StructInfo struct {
	Name     string   `json:"name"`
	File     string   `json:"file"`
	AppField string   `json:"app_field"`
	Methods  []string `json:"methods"` // methods that use the App field
}

type Report struct {
	Funcs        []*FuncInfo            `json:"funcs"`
	Structs      map[string]*StructInfo `json:"structs"`
	FactoryDeps  map[string][]string    `json:"factory_deps"`  // factory -> App-taking helpers
	FactoryTypes map[string][]string    `json:"factory_types"` // factory -> App-holding structs
	StructDeps   map[string][]string    `json:"struct_deps"`   // struct -> App-taking helpers
}

func main() {
	dir := os.Args[1]
	if len(os.Args) > 2 && os.Args[2] == "--bindings" {
		runBindings(dir, len(os.Args) > 3 && os.Args[3] == "--json")
		return
	}
	fset := token.NewFileSet()
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var parsed []*ast.File
	paths := map[*ast.File]string{}
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		parsed = append(parsed, f)
		paths[f] = p
	}

	// pass 1: *App-taking functions, and types with an *App field
	appParam := map[string]string{}
	info := map[string]*FuncInfo{}
	var order []string
	structs := map[string]*StructInfo{}
	for _, f := range parsed {
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Body == nil || decl.Type.Params == nil {
					continue
				}
				for _, prm := range decl.Type.Params.List {
					star, ok := prm.Type.(*ast.StarExpr)
					if !ok {
						continue
					}
					id, ok := star.X.(*ast.Ident)
					if !ok || id.Name != "App" || len(prm.Names) == 0 {
						continue
					}
					appParam[decl.Name.Name] = prm.Names[0].Name
					fi := &FuncInfo{Name: decl.Name.Name, File: paths[f],
						TakesApp: true, IsFactory: strings.HasSuffix(decl.Name.Name, "Ports")}
					// a method on an App-holding struct also counts
					if decl.Recv != nil && len(decl.Recv.List) > 0 {
						if tn := recvName(decl.Recv.List[0].Type); tn != "" {
							if s, ok := structs[tn]; ok {
								s.Methods = append(s.Methods, decl.Name.Name)
							}
						}
					}
					info[decl.Name.Name] = fi
					order = append(order, decl.Name.Name)
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
						if star, ok := fld.Type.(*ast.StarExpr); ok {
							if id, ok := star.X.(*ast.Ident); ok && id.Name == "App" && len(fld.Names) > 0 {
								structs[ts.Name.Name] = &StructInfo{Name: ts.Name.Name, File: paths[f], AppField: fld.Names[0].Name}
							}
						}
					}
				}
			}
		}
	}

	// second sweep: methods on App-holding structs (declared after the type)
	for _, f := range parsed {
		for _, d := range f.Decls {
			decl, ok := d.(*ast.FuncDecl)
			if !ok || decl.Body == nil || decl.Recv == nil || len(decl.Recv.List) == 0 {
				continue
			}
			tn := recvName(decl.Recv.List[0].Type)
			s, ok := structs[tn]
			if !ok {
				continue
			}
			found := false
			for _, m := range s.Methods {
				if m == decl.Name.Name {
					found = true
				}
			}
			if !found {
				s.Methods = append(s.Methods, decl.Name.Name)
			}
		}
	}

	// pass 2: bodies
	for _, f := range parsed {
		for _, d := range f.Decls {
			decl, ok := d.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				continue
			}
			var varName string
			var fi *FuncInfo
			if v, ok := appParam[decl.Name.Name]; ok {
				varName, fi = v, info[decl.Name.Name]
			} else if decl.Recv != nil && len(decl.Recv.List) > 0 {
				if s, ok := structs[recvName(decl.Recv.List[0].Type)]; ok {
					varName = s.AppField
					fi = &FuncInfo{Name: recvName(decl.Recv.List[0].Type) + "." + decl.Name.Name}
				}
			}
			if varName == "" || fi == nil {
				continue
			}
			direct := map[string]bool{}
			bindings := map[string]bool{}
			calls := map[string]bool{}
			inst := map[string]bool{}
			ast.Inspect(decl.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if id, ok := x.X.(*ast.Ident); ok && id.Name == varName {
						if x.Sel.Name != "bindings" {
							direct[x.Sel.Name] = true
						}
					}
					if inner, ok := x.X.(*ast.SelectorExpr); ok {
						if id, ok := inner.X.(*ast.Ident); ok && id.Name == varName && inner.Sel.Name == "bindings" {
							bindings[x.Sel.Name] = true
						}
					}
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok {
						if _, isApp := appParam[id.Name]; isApp && id.Name != decl.Name.Name {
							calls[id.Name] = true
						}
					}
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); ok {
						if _, isAppType := structs[id.Name]; isAppType {
							inst[id.Name] = true
						}
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
			for k := range inst {
				fi.Instantiates = append(fi.Instantiates, k)
			}
			sort.Strings(fi.Direct)
			sort.Strings(fi.Bindings)
			sort.Strings(fi.CallsApp)
			sort.Strings(fi.Instantiates)
			if fi.TakesApp {
				info[decl.Name.Name] = fi
			} else {
				// method of an App-holding struct
				key := fi.Name
				info[key] = fi
				order = append(order, key)
			}
		}
	}

	rep := &Report{Funcs: []*FuncInfo{}, Structs: structs,
		FactoryDeps: map[string][]string{}, FactoryTypes: map[string][]string{},
		StructDeps: map[string][]string{}}
	for _, n := range order {
		if fi, ok := info[n]; ok {
			rep.Funcs = append(rep.Funcs, fi)
		}
	}

	// struct -> helpers its methods call (plus structs they instantiate)
	for name, s := range structs {
		set := map[string]bool{}
		for _, m := range s.Methods {
			if fi, ok := info[name+"."+m]; ok {
				for _, c := range fi.CallsApp {
					set[c] = true
				}
			}
		}
		var out []string
		for k := range set {
			out = append(out, k)
		}
		sort.Strings(out)
		rep.StructDeps[name] = out
	}

	// transitive closure per factory over calls and instantiations, walking
	// into struct methods
	var close func(seenF, seenS map[string]bool, f *FuncInfo)
	close = func(seenF, seenS map[string]bool, f *FuncInfo) {
		for _, c := range f.CallsApp {
			if seenF[c] {
				continue
			}
			seenF[c] = true
			if cf, ok := info[c]; ok && cf.TakesApp {
				close(seenF, seenS, cf)
			}
		}
		for _, t := range f.Instantiates {
			if seenS[t] {
				continue
			}
			seenS[t] = true
			if s, ok := structs[t]; ok {
				for _, m := range s.Methods {
					if mf, ok := info[t+"."+m]; ok {
						close(seenF, seenS, mf)
					}
				}
			}
		}
	}

	for _, f := range rep.Funcs {
		if !f.IsFactory {
			continue
		}
		seenF, seenS := map[string]bool{}, map[string]bool{}
		close(seenF, seenS, f)
		rep.FactoryDeps[f.Name] = keys(seenF)
		rep.FactoryTypes[f.Name] = keys(seenS)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	enc.Encode(rep)
	fmt.Fprintf(os.Stderr, "funcs=%d structs=%d factories=%d\n", len(rep.Funcs), len(structs), len(rep.FactoryDeps))
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}
