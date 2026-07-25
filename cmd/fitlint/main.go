// Command fitlint implements the AST- and text-based checks orchestrated by
// scripts/fitness.sh (docs/PLAN.md Task 18 / SKP-16):
//
//   - enum      — flags any const block declaring 3+ of the six canonical
//     workflow status words (Constitution C1) outside internal/state.
//   - term      — flags the superseded label TEN_X_BRANCHES_READY appearing
//     outside its documented allowed locations.
//   - doclinks  — flags relative Markdown links that don't resolve to a real
//     file or directory.
//
// This tool intentionally uses only go/parser + go/ast from the standard
// library rather than golang.org/x/tools/go/analysis: the checks needed here
// are simple single-file AST inspections, and stdlib already covers them
// without adding a new module dependency.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sixStatusWords are the exact literal values of Constitution C1's six
// canonical workflow statuses (state-model.md §2). Matching is exact, not
// substring, so unrelated const blocks that merely share a literal (e.g. a
// generic "FAILED" flag in an unrelated enum) are not flagged unless three or
// more of these exact words co-occur in the same const block.
var sixStatusWords = map[string]bool{
	"PENDING":   true,
	"RUNNING":   true,
	"WAITING":   true,
	"SUCCEEDED": true,
	"FAILED":    true,
	"CANCELLED": true,
}

// termAllowlist is the fixed set of repo-relative files the superseded label
// TEN_X_BRANCHES_READY is permitted to appear in (docs/foundry/docs/
// governance/documentation-rules.md: "outside this mapping table, the
// migration map, or the changelog"). docs/PLAN.md is included because it is
// this repo's own task-history record and functions as a changelog entry for
// Task 5/Task 18; docs/foundry/V12_REVIEW_REPORT.md is the dated review
// record that first retired the term, functioning the same way.
var termAllowlist = map[string]bool{
	"internal/state/alias.go":                       true,
	"internal/state/status.go":                      true,
	"cmd/fitlint/main.go":                           true,
	"docs/PLAN.md":                                  true,
	"docs/foundry/CHANGELOG.md":                     true,
	"docs/foundry/V12_REVIEW_REPORT.md":             true,
	"docs/foundry/docs/architecture/state-model.md": true,
	"docs/foundry/docs/MIGRATION_MAP_V11_TO_V12.md": true,
}

const supersededTerm = "TEN_X_BRANCHES_READY"

// skipDirNames are directories never walked by any fitlint check.
var skipDirNames = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"vendor":        true,
	"fitness_seeds": true, // deliberately-violating fixtures; scanned directly by scripts/fitness_selftest.sh, never as part of a real scan
}

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}
	cmd, roots := os.Args[1], os.Args[2:]

	var violations []string
	var err error
	switch cmd {
	case "enum":
		violations, err = checkEnum(roots)
	case "term":
		violations, err = checkTerm(roots)
	case "doclinks":
		violations, err = checkDocLinks(roots)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fitlint:", err)
		os.Exit(2)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		for _, v := range violations {
			fmt.Println(v)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: fitlint <enum|term|doclinks> <root>...")
}

// walkFiles walks roots, invoking fn for every regular file whose name
// matches suffix (e.g. ".go" or ".md"). Skips skipDirNames anywhere in the
// tree.
func walkFiles(roots []string, suffix string, fn func(path string) error) error {
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if skipDirNames[info.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, suffix) {
				return nil
			}
			return fn(filepath.ToSlash(path))
		})
		if err != nil {
			return fmt.Errorf("walk %s: %w", root, err)
		}
	}
	return nil
}

// checkEnum implements rule (a): any const block declaring 3+ of the six
// canonical status words outside internal/state.
func checkEnum(roots []string) ([]string, error) {
	var violations []string
	fset := token.NewFileSet()
	err := walkFiles(roots, ".go", func(path string) error {
		if strings.Contains(path, "internal/state/") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			matched := map[string]bool{}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val := strings.Trim(lit.Value, `"`+"`")
					if sixStatusWords[val] {
						matched[val] = true
					}
				}
			}
			if len(matched) >= 3 {
				pos := fset.Position(gd.Pos())
				violations = append(violations, fmt.Sprintf(
					"%s:%d: const block declares %d canonical status words (%s) outside internal/state",
					path, pos.Line, len(matched), joinKeys(matched)))
			}
		}
		return nil
	})
	return violations, err
}

func joinKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// checkTerm implements rule (b): the superseded label TEN_X_BRANCHES_READY
// must not appear outside termAllowlist.
func checkTerm(roots []string) ([]string, error) {
	var violations []string
	err := walkFiles(roots, "", func(path string) error {
		rel := strings.TrimPrefix(path, "./")
		if termAllowlist[rel] {
			return nil
		}
		if skipBinaryLike(path) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil // unreadable/gone mid-walk; not this tool's concern
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if strings.Contains(scanner.Text(), supersededTerm) {
				violations = append(violations, fmt.Sprintf("%s:%d: superseded term %s outside allowlist", rel, line, supersededTerm))
			}
		}
		return nil
	})
	return violations, err
}

// skipBinaryLike skips file extensions that are never text-inspected by this
// check (git objects, images, compiled binaries) to keep the walk fast and
// avoid scanning non-text bytes.
func skipBinaryLike(path string) bool {
	switch filepath.Ext(path) {
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".woff", ".woff2", ".pdf", ".exe", ".so", ".a", ".o":
		return true
	}
	return false
}

// checkDocLinks implements rule (d): every relative Markdown link must
// resolve to a real file or directory. External links (http/https/mailto)
// and pure same-file anchors are intentionally not checked — validating
// anchor fragments against heading slugs is out of scope for v0 and would
// risk false failures on legitimate cross-references.
func checkDocLinks(roots []string) ([]string, error) {
	var violations []string
	err := walkFiles(roots, ".md", func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		dir := filepath.Dir(path)
		inFence := false
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			for _, target := range extractLinkTargets(line) {
				if isExternal(target) {
					continue
				}
				target = strings.SplitN(target, "#", 2)[0]
				if target == "" {
					continue // pure same-file anchor
				}
				resolved := target
				if !filepath.IsAbs(target) {
					resolved = filepath.Join(dir, target)
				} else {
					resolved = "." + target
				}
				if _, err := os.Stat(resolved); err != nil {
					violations = append(violations, fmt.Sprintf("%s:%d: dead link %q (resolved %s)", path, i+1, target, resolved))
				}
			}
		}
		return nil
	})
	return violations, err
}

func isExternal(target string) bool {
	for _, prefix := range []string{"http://", "https://", "mailto:", "//"} {
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}
	return false
}

// extractLinkTargets returns the parenthesized target of every Markdown
// inline link `[text](target)` on a line. A small hand-rolled scanner is used
// instead of regexp so nested brackets in link text (rare but legal) don't
// need a lookahead-free regex workaround.
func extractLinkTargets(line string) []string {
	var targets []string
	for i := 0; i < len(line); i++ {
		if line[i] != '[' {
			continue
		}
		closeBracket := strings.IndexByte(line[i:], ']')
		if closeBracket == -1 {
			break
		}
		closeBracket += i
		if closeBracket+1 >= len(line) || line[closeBracket+1] != '(' {
			continue
		}
		closeParen := strings.IndexByte(line[closeBracket+1:], ')')
		if closeParen == -1 {
			continue
		}
		closeParen += closeBracket + 1
		target := line[closeBracket+2 : closeParen]
		targets = append(targets, target)
		i = closeParen
	}
	return targets
}
