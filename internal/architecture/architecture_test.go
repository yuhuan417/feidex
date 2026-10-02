package architecture

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "feidex"

func TestLayerImportRules(t *testing.T) {
	root := repositoryRoot(t)
	rules := []struct {
		prefix string
		banned []string
	}{
		{prefix: filepath.Join("internal", "domain"), banned: []string{
			modulePath + "/internal/application",
			modulePath + "/internal/adapter",
			modulePath + "/internal/runtime",
			modulePath + "/internal/app",
			modulePath + "/internal/feishu",
			modulePath + "/internal/codexrpc",
			modulePath + "/internal/claudecli",
			modulePath + "/internal/state",
			modulePath + "/internal/config",
		}},
		{prefix: filepath.Join("internal", "application"), banned: []string{
			modulePath + "/internal/adapter",
			modulePath + "/internal/runtime",
			modulePath + "/internal/state",
			modulePath + "/internal/config",
			modulePath + "/internal/app",
			modulePath + "/internal/feishu",
			modulePath + "/internal/codexrpc",
			modulePath + "/internal/claudecli",
		}},
		{prefix: filepath.Join("internal", "adapter"), banned: []string{
			modulePath + "/internal/app",
		}},
		{prefix: filepath.Join("internal", "feishu"), banned: []string{
			modulePath + "/internal/app",
		}},
	}

	for _, rule := range rules {
		rule := rule
		t.Run(filepath.ToSlash(rule.prefix), func(t *testing.T) {
			violations, err := importsUnder(root, rule.prefix, rule.banned)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) > 0 {
				t.Fatalf("forbidden imports:\n%s", strings.Join(violations, "\n"))
			}
		})
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The test package lives at internal/architecture.
	return filepath.Clean(filepath.Join(root, "..", ".."))
}

func importsUnder(root, relative string, banned []string) ([]string, error) {
	var violations []string
	base := filepath.Join(root, relative)
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, prefix := range banned {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					violations = append(violations, fmt.Sprintf("%s imports %s", path, importPath))
					break
				}
			}
		}
		return nil
	})
	sort.Strings(violations)
	return violations, err
}
