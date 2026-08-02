// Command fitlint implements the AST- and text-based checks orchestrated by
// scripts/fitness.sh (docs/PLAN.md Task 18 / SKP-16):
//
//   - enum      — flags any const block declaring 3+ of the six canonical
//     workflow status words (Constitution C1) outside internal/state.
//   - term      — flags banned/superseded labels (e.g. TEN_X_BRANCHES_READY,
//     synthetic organization-brand token) appearing outside each term's
//     per-term allowlist (docs/PLAN.md Task 131).
//   - doclinks  — flags relative Markdown links that don't resolve to a real
//     file or directory, and (docs/PLAN.md Task 37 / FND-18) a "#anchor"
//     fragment that doesn't match any heading's GitHub-style slug in the
//     resolved target file. See checkDocLinks.
//   - authority — docs/PLAN.md Task 28 (FND-09): compile-time-graph proof of
//     Constitution C4/C5's authority import boundary. See checkAuthority.
//   - secretsleak — docs/PLAN.md Task 35 (FND-16): flags plaintext-secret-
//     shaped strings (GitHub tokens, Telegram bot tokens, Anthropic API
//     keys, spilled age identities) anywhere in the tree. See
//     checkSecretsLeak.
//   - mermaidid — docs/PLAN.md Task 37 (FND-18): flags a Mermaid diagram
//     "D-<n>" heading ID reused in more than one place. See
//     checkMermaidDupID.
//   - contract — docs/PLAN.md Task 37 (FND-18): flags a "Contract: <Name>"
//     heading whose <Name> is defined normatively in more than one
//     document (single-source rule — other documents must link, not
//     redefine). See checkContract.
//   - containers — docs/PLAN.md Task 37 (FND-18): flags any
//     Dockerfile*/docker-compose*.y*ml found on disk that lacks a matching
//     row in CLAUDE.md's §C container topology table. See checkContainers.
//
// This tool intentionally uses only go/parser + go/ast from the standard
// library rather than golang.org/x/tools/go/analysis: the checks needed here
// are simple single-file AST inspections, and stdlib already covers them
// without adding a new module dependency. The authority check is the one
// exception that shells out to the `go` binary itself (`go list -json`),
// because building a same-repository import graph correctly (direct edges,
// not a transitive closure) is what `go list` already does, precisely, and
// re-deriving it from raw AST parsing would just be a worse copy of the same
// data go/build already computes.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
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

// bannedTerm is one repository-wide prohibited string with its own allowlist.
// Per-term allowlists prevent a newly-added term from silently inheriting
// another term's exemptions (docs/PLAN.md Task 131 / DOC-01).
type bannedTerm struct {
	term      string
	allowlist map[string]bool
}

// bannedTerms is the authoritative list of prohibited labels/names scanned by
// the `term` subcommand. Each entry carries its own allowlist keyed by
// repo-relative file path.
var bannedTerms = []bannedTerm{
	{
		term: "TEN_X_BRANCHES_READY",
		allowlist: map[string]bool{
			"internal/state/alias.go":                       true,
			"internal/state/status.go":                      true,
			"cmd/fitlint/main.go":                           true,
			"docs/PLAN.md":                                  true,
			"docs/foundry/CHANGELOG.md":                     true,
			"docs/foundry/V12_REVIEW_REPORT.md":             true,
			"docs/foundry/docs/architecture/state-model.md": true,
			"docs/foundry/docs/MIGRATION_MAP_V11_TO_V12.md": true,
		},
	},
	{
		// Synthetic banned organization-brand token for the de-branding lint
		// (docs/PLAN.md Task 131). The plan must name no specific company;
		// this token exists only so fitness_selftest can prove the per-term
		// allowlist catches brand leakage.
		term: "AcmeCorp",
		allowlist: map[string]bool{
			"cmd/fitlint/main.go": true, // documents the banned-term list itself
		},
	},
}

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
	case "authority":
		violations, err = checkAuthority(roots)
	case "secretsleak":
		violations, err = checkSecretsLeak(roots)
	case "mermaidid":
		violations, err = checkMermaidDupID(roots)
	case "contract":
		violations, err = checkContract(roots)
	case "containers":
		violations, err = checkContainers(roots)
	case "missionloop":
		violations, err = checkMissionLoopContract(roots)
	case "capability":
		violations, err = checkCapabilityStaleness(roots)
	case "research-boundary":
		violations, err = checkResearchBoundary(roots)
	case "plan-validation":
		violations, err = checkPlanValidation(roots)
	case "plan-topology":
		violations, err = checkPlanTopology(roots)
	case "subprocess":
		violations, err = checkKernelSubprocess(roots)
	case "env":
		violations, err = checkExecutorEnv(roots)
	case "stale-task-comment":
		violations, err = checkStaleTaskComments(roots)
	case "test-source-write":
		violations, err = checkTestSourceWrite(roots)
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
	fmt.Fprintln(os.Stderr, "usage: fitlint <enum|term|doclinks|authority|secretsleak|mermaidid|contract|containers|missionloop|capability|research-boundary|plan-validation|plan-topology|subprocess|env|stale-task-comment|test-source-write> <root>...")
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

// checkTerm implements rule (b): each banned term must not appear outside its
// own per-term allowlist (docs/PLAN.md Task 131: per-term, not per-file).
func checkTerm(roots []string) ([]string, error) {
	var violations []string
	err := walkFiles(roots, "", func(path string) error {
		rel := strings.TrimPrefix(path, "./")
		if skipBinaryLike(path) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil // unreadable/gone mid-walk; not this tool's concern
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			for _, bt := range bannedTerms {
				if !strings.Contains(text, bt.term) {
					continue
				}
				if bt.allowlist[rel] {
					continue
				}
				violations = append(violations, fmt.Sprintf("%s:%d: banned term %s outside allowlist", rel, line, bt.term))
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
	base := filepath.Base(path)
	switch base {
	case "fitlint", "foundry", "foundryd", "startplan", "execonce":
		return true
	}
	switch filepath.Ext(path) {
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".woff", ".woff2", ".pdf", ".exe", ".so", ".a", ".o", ".wasm":
		return true
	}
	// Extensionless files that look like Mach-O/ELF binaries: skip on magic.
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	var hdr [4]byte
	n, _ := f.Read(hdr[:])
	if n >= 4 {
		// ELF
		if hdr[0] == 0x7f && hdr[1] == 'E' && hdr[2] == 'L' && hdr[3] == 'F' {
			return true
		}
		// Mach-O 64-bit LE / BE / fat
		if (hdr[0] == 0xcf && hdr[1] == 0xfa && hdr[2] == 0xed && hdr[3] == 0xfe) ||
			(hdr[0] == 0xfe && hdr[1] == 0xed && hdr[2] == 0xfa && hdr[3] == 0xcf) ||
			(hdr[0] == 0xca && hdr[1] == 0xfe && hdr[2] == 0xba && hdr[3] == 0xbe) {
			return true
		}
	}
	return false
}

// checkDocLinks implements rule (d): every relative Markdown link must
// resolve to a real file or directory, and (docs/PLAN.md Task 37 / FND-18)
// every "#anchor" fragment — same-file or cross-file — must match a real
// heading's GitHub-style slug in the resolved target file. External links
// (http/https/mailto) are not checked. Anchors pointing into a file this
// call didn't walk (e.g. a non-.md file, or a .md file outside roots) are
// left unchecked rather than guessed at.
func checkDocLinks(roots []string) ([]string, error) {
	slugsByFile := map[string]map[string]bool{}
	err := walkFiles(roots, ".md", func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		slugsByFile[path] = headingSlugs(data)
		return nil
	})
	if err != nil {
		return nil, err
	}

	var violations []string
	err = walkFiles(roots, ".md", func(path string) error {
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
				parts := strings.SplitN(target, "#", 2)
				filePart := parts[0]
				anchor := ""
				if len(parts) == 2 {
					anchor = strings.ToLower(parts[1])
				}

				targetPath := path // pure same-file anchor default
				if filePart != "" {
					resolved := filePart
					if filepath.IsAbs(filePart) {
						resolved = "." + filePart
					} else {
						resolved = filepath.Join(dir, filePart)
					}
					resolved = filepath.ToSlash(resolved)
					if _, err := os.Stat(resolved); err != nil {
						violations = append(violations, fmt.Sprintf("%s:%d: dead link %q (resolved %s)", path, i+1, target, resolved))
						continue
					}
					targetPath = resolved
				}

				if anchor == "" {
					continue
				}
				slugs, tracked := slugsByFile[targetPath]
				if !tracked {
					continue // not a walked .md file — can't verify, don't guess
				}
				if !slugs[anchor] {
					violations = append(violations, fmt.Sprintf("%s:%d: dead anchor %q in link %q (no matching heading in %s)", path, i+1, anchor, target, targetPath))
				}
			}
		}
		return nil
	})
	return violations, err
}

// restrictedImportSuffix and the two importer-boundary suffixes below name
// the three packages docs/PLAN.md Task 28 (FND-09) constrains, expressed as
// the trailing path segments of their import path (e.g. an import path
// ending in "/internal/kernel", or equal to "internal/kernel" for the
// (unlikely) case of the module root itself being that package). Matching
// on the trailing segments — not the fully module-qualified path — is what
// lets rule (b) below fire the instant a real internal/pec package exists
// (Task 56), without this file ever needing to change, and lets this task's
// own seeded fixtures prove the same rule from a path nested under
// test/fitness_seeds rather than the real location.
const (
	restrictedImportSuffix = "internal/scm/write"
	kernelImportSuffix     = "internal/kernel"
	pecImportSuffix        = "internal/pec"
)

// goListPackage is the subset of `go list -json`'s per-package object this
// check needs: ImportPath identifies the package itself; Imports/
// TestImports/XTestImports are its DIRECT imports only (not the transitive
// closure `go list -deps` would return for a single target) — the
// distinction matters: internal/kernel legitimately imports
// internal/scm/write, so any other package that merely imports
// internal/kernel (and not internal/scm/write itself) must NOT be flagged.
// Checking only each package's own direct Imports/TestImports/XTestImports,
// rather than walking a transitive dependency set, is what keeps that
// legitimate case out of the violation list.
type goListPackage struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

// checkAuthority implements docs/PLAN.md Task 28 (FND-09): a `go list`-based
// import-graph assertion proving two halves of Constitution C4/C5's
// authority boundary:
//
//   - (a) internal/scm/write is imported — in non-test or test code alike —
//     only by internal/kernel (or a subpackage of it). Any other package
//     importing it directly is a violation.
//   - (b) internal/pec (or any subpackage of it — Task 56 has not built the
//     package yet, but this rule is keyed on the path, not on the package's
//     current existence, so it activates automatically the moment Task 56
//     adds files there) imports neither internal/kernel nor
//     internal/scm/write.
//
// roots are `go list` package patterns (e.g. "./internal/...", not bare
// directory paths), since this check needs the module's real import graph,
// not a filesystem walk.
func checkAuthority(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, fmt.Errorf("authority: at least one go-list package pattern required")
	}
	pkgs, err := goListJSON(patterns)
	if err != nil {
		return nil, err
	}

	var violations []string
	for _, pkg := range pkgs {
		imports := allImports(pkg)
		isKernel := hasPathSuffix(pkg.ImportPath, kernelImportSuffix)
		isPEC := hasPathSuffix(pkg.ImportPath, pecImportSuffix)

		for _, imp := range imports {
			if imp == pkg.ImportPath {
				// A package's own external (black-box "_test") test files
				// legitimately import the package under test itself — e.g.
				// internal/scm/write's own github_test.go is package
				// write_test and imports internal/scm/write. That is
				// self-testing, not an outside import, so it is not a
				// boundary violation under either rule.
				continue
			}
			if hasPathSuffix(imp, restrictedImportSuffix) && !isKernel {
				violations = append(violations, fmt.Sprintf(
					"%s: imports %s directly — only %s may (Constitution C4)",
					pkg.ImportPath, imp, kernelImportSuffix))
			}
			if isPEC && (hasPathSuffix(imp, kernelImportSuffix) || hasPathSuffix(imp, restrictedImportSuffix)) {
				violations = append(violations, fmt.Sprintf(
					"%s: imports %s — %s must never import kernel or scm/write (Constitution C5)",
					pkg.ImportPath, imp, pecImportSuffix))
			}
		}
	}
	sort.Strings(violations)
	return violations, nil
}

// allImports returns pkg's direct imports across regular, internal-test
// (_test.go, same package), and external-test (_test.go, "_test" package)
// files combined — an illegitimate import of internal/scm/write from a test
// file outside internal/kernel is exactly as much a C4 violation as one from
// production code.
func allImports(pkg goListPackage) []string {
	all := make([]string, 0, len(pkg.Imports)+len(pkg.TestImports)+len(pkg.XTestImports))
	all = append(all, pkg.Imports...)
	all = append(all, pkg.TestImports...)
	all = append(all, pkg.XTestImports...)
	return all
}

// hasPathSuffix reports whether importPath is suffix itself, or ends in
// "/"+suffix (suffix is that package), or contains "/"+suffix+"/" (suffix is
// an ancestor directory of importPath — e.g. importPath is a subpackage of
// suffix, or, for the seeded fixtures under test/fitness_seeds, a path
// deliberately nested so its own import path contains the same trailing
// segment sequence as the real package it stands in for). Matching is
// boundary-safe: "internal/pecking-order" does not match suffix
// "internal/pec".
func hasPathSuffix(importPath, suffix string) bool {
	return importPath == suffix ||
		strings.HasSuffix(importPath, "/"+suffix) ||
		strings.Contains(importPath, "/"+suffix+"/")
}

// goListJSON shells out to `go list -json <patterns...>` and decodes its
// output, which is a stream of concatenated JSON objects (one per package),
// not a JSON array — hence the streaming json.Decoder loop rather than a
// single json.Unmarshal.
func goListJSON(patterns []string) ([]goListPackage, error) {
	args := append([]string{"list", "-json"}, patterns...)
	cmd := exec.Command("go", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list -json %s: %w: %s", strings.Join(patterns, " "), err, stderr.String())
	}

	var pkgs []goListPackage
	dec := json.NewDecoder(&stdout)
	for {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode go list -json output: %w", err)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

// secretPatterns are the plaintext-secret shapes docs/PLAN.md Task 35
// (FND-16)'s leak scanner flags anywhere in the tree (repo source, logs,
// evidence fixtures — all just files under the walked roots). Each
// pattern is deliberately shaped tightly enough that ordinary test
// placeholders ("test-token", "sk-should-be-visible" in
// internal/executor/claudecode's own leak test, etc.) never match — only
// strings with the real provider's actual token shape do:
//
//   - GitHub PATs: classic (ghp_/gho_/ghu_/ghs_/ghr_) and fine-grained
//     (github_pat_) prefixes, each followed by their real minimum length.
//   - Anthropic API keys: sk-ant- prefix.
//   - Telegram bot tokens: <6-10 digit bot id>:<35-char secret>.
//   - age identities: a plaintext AGE-SECRET-KEY-1... spilled outside an
//     encrypted secrets.age file is exactly the key-management failure
//     internal/secrets/filestore exists to prevent.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bghp_[A-Za-z0-9]{36}\b`),
	regexp.MustCompile(`\bgho_[A-Za-z0-9]{36}\b`),
	regexp.MustCompile(`\bghu_[A-Za-z0-9]{36}\b`),
	regexp.MustCompile(`\bghs_[A-Za-z0-9]{36}\b`),
	regexp.MustCompile(`\bghr_[A-Za-z0-9]{36}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\b[0-9]{6,10}:[A-Za-z0-9_-]{35}\b`),
	regexp.MustCompile(`\bAGE-SECRET-KEY-1[A-Z0-9]{20,}\b`),
}

// secretsLeakAllowlist is the fixed set of repo-relative files permitted
// to contain secret-shaped strings: this file documents the exact
// patterns as regex source (which, by construction, never itself matches
// its own compiled patterns — see the patterns' own doc comment), and
// docs/PLAN.md is this repo's own task-history record, which quotes this
// check's name and purpose but never a real secret value.
var secretsLeakAllowlist = map[string]bool{
	"cmd/fitlint/main.go": true,
}

// checkSecretsLeak implements Task 35's leak scanner: no file under roots
// may contain a plaintext string matching any of secretPatterns.
func checkSecretsLeak(roots []string) ([]string, error) {
	var violations []string
	err := walkFiles(roots, "", func(path string) error {
		rel := strings.TrimPrefix(path, "./")
		if secretsLeakAllowlist[rel] {
			return nil
		}
		if skipBinaryLike(path) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil // unreadable/gone mid-walk; not this tool's concern
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			for _, pat := range secretPatterns {
				if pat.MatchString(text) {
					violations = append(violations, fmt.Sprintf("%s:%d: plaintext-secret-shaped string matching %s", rel, line, pat.String()))
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

// headingSlugs returns the GitHub-style anchor slug for every ATX heading in
// data (outside fenced code blocks), in document order, with GitHub's own
// duplicate-heading suffixing (a slug's second occurrence in the same
// document becomes slug-1, the third slug-2, and so on). This is a close
// approximation of GitHub's own rendering, not a byte-perfect
// reimplementation of it — sufficient for checkDocLinks's purpose. Nothing
// in this repo's real docs currently links to an anchor at all (verified by
// grep before this check was added), so there is no existing content this
// approximation could false-positive against; its correctness is instead
// proven by the seeded violation in test/fitness_seeds/doclink.
func headingSlugs(data []byte) map[string]bool {
	slugs := map[string]bool{}
	seen := map[string]int{}
	inFence := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		i := 0
		for i < len(trimmed) && trimmed[i] == '#' {
			i++
		}
		if i == 0 || i > 6 || i >= len(trimmed) || trimmed[i] != ' ' {
			continue // not a valid ATX heading (too many '#', or no space after them)
		}
		text := strings.TrimSpace(trimmed[i:])
		text = strings.TrimSpace(strings.TrimRight(text, "#"))
		if text == "" {
			continue
		}
		slug := slugify(text)
		if slug == "" {
			continue
		}
		if n := seen[slug]; n > 0 {
			seen[slug] = n + 1
			slug = fmt.Sprintf("%s-%d", slug, n)
		} else {
			seen[slug] = 1
		}
		slugs[slug] = true
	}
	return slugs
}

// slugify approximates GitHub's heading-to-anchor algorithm: lowercase, drop
// every rune that isn't a letter, digit, hyphen, or underscore (punctuation,
// backticks, em-dashes, etc. are dropped, not replaced), then collapse
// whitespace runs into single hyphens.
func slugify(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '\t':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), "-")
}

// mermaidIDPattern matches a Markdown ATX heading whose text begins with the
// "D-<number>" diagram-ID convention this docs set uses for every Mermaid
// diagram section (docs/foundry/docs/governance/documentation-rules.md: "a
// duplicate Mermaid diagram ID is introduced" fails CI). The heading level
// varies (## or ###) across files, so any ATX level is matched.
var mermaidIDPattern = regexp.MustCompile(`^#{1,6}\s+(D-\d+)\b`)

// checkMermaidDupID implements the duplicate-Mermaid-diagram-ID detector
// (docs/PLAN.md Task 37 / FND-18): every "D-<n>" heading ID in this docs set
// identifies exactly one diagram section; the same ID reused in a second
// heading anywhere is a violation — whether the second occurrence is an
// accidental collision or a copy-pasted section, both break the ID as a
// unique cross-reference target.
func checkMermaidDupID(roots []string) ([]string, error) {
	type occurrence struct {
		path string
		line int
	}
	seen := map[string][]occurrence{}
	err := walkFiles(roots, ".md", func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		inFence := false
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			m := mermaidIDPattern.FindStringSubmatch(trimmed)
			if m == nil {
				continue
			}
			seen[m[1]] = append(seen[m[1]], occurrence{path: path, line: i + 1})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var violations []string
	for id, occs := range seen {
		if len(occs) < 2 {
			continue
		}
		locs := make([]string, 0, len(occs))
		for _, o := range occs {
			locs = append(locs, fmt.Sprintf("%s:%d", o.path, o.line))
		}
		sort.Strings(locs)
		violations = append(violations, fmt.Sprintf("duplicate Mermaid diagram ID %s at %s", id, strings.Join(locs, ", ")))
	}
	sort.Strings(violations)
	return violations, nil
}

// contractHeadingPattern matches this repo's explicit single-source-contract
// heading convention: a Markdown heading of the literal form
// "Contract: <Name>" (case-insensitive on the "Contract:" label) marks
// <Name> as normatively defined in that document.
// docs/foundry/docs/governance/documentation-rules.md: "a contract is
// defined normatively in more than one document (single-source rule — other
// documents link, never redefine)". This is a heuristic keyed on an explicit
// heading label, not a semantic-equivalence check across arbitrary prose.
var contractHeadingPattern = regexp.MustCompile(`(?i)^#{1,6}\s+contract:\s*(.+?)\s*$`)

// checkContract implements the single-source contract heuristic
// (docs/PLAN.md Task 37 / FND-18): the same "Contract: <Name>" heading must
// not be normatively defined in more than one document.
func checkContract(roots []string) ([]string, error) {
	type occurrence struct {
		path string
		line int
	}
	seen := map[string][]occurrence{}
	err := walkFiles(roots, ".md", func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		inFence := false
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			m := contractHeadingPattern.FindStringSubmatch(trimmed)
			if m == nil {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(strings.TrimRight(m[1], "#")))
			if name == "" {
				continue
			}
			seen[name] = append(seen[name], occurrence{path: path, line: i + 1})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var violations []string
	for name, occs := range seen {
		if len(occs) < 2 {
			continue
		}
		locs := make([]string, 0, len(occs))
		for _, o := range occs {
			locs = append(locs, fmt.Sprintf("%s:%d", o.path, o.line))
		}
		sort.Strings(locs)
		violations = append(violations, fmt.Sprintf("contract %q defined normatively in more than one document: %s", name, strings.Join(locs, ", ")))
	}
	sort.Strings(violations)
	return violations, nil
}

// containerFileAllowlist maps every known Dockerfile*/docker-compose*.y*ml
// path (repo-relative, slash-separated) to the exact "Image" column value it
// must appear under in CLAUDE.md's §C container topology table
// (.ai/instructions/build-and-test.md is the canonical `.ai/` source;
// CLAUDE.md composes it verbatim — see the Prompt Caching stable-prefix
// rule). A path present here whose mapped image string no longer appears in
// CLAUDE.md's table is exactly as much a violation as a path absent from
// this map altogether: both mean an image lineage exists on disk without
// the single governing table row docs/PLAN.md §C requires (docs/PLAN.md
// Task 37 / FND-18 — "an untracked Dockerfile fails CI by name, not just
// review").
var containerFileAllowlist = map[string]string{
	"deploy/Dockerfile.dev":             "`dev`",
	"deploy/docker-compose.yaml":        "`postgres`, `temporal`",
	"deploy/docker-compose.prod.yaml":   "`foundry` (release)",
	"deploy/images/executor.Dockerfile": "`foundry-executor-sandbox`",
	"templates/product/Dockerfile":      "product template's own image",
}

// containerFilePattern matches a Dockerfile*/docker-compose*.y*ml basename
// wherever "dockerfile" occurs in it (not just as a literal prefix), so a
// file like Task 34's deploy/images/executor.Dockerfile — "Dockerfile" as a
// suffix, not a prefix — is still recognized as a container image lineage
// that needs a table row, exactly as a literal "Dockerfile.foo" would be.
var containerFilePattern = regexp.MustCompile(`(?i)dockerfile|^docker-compose.*\.ya?ml$`)

// containerTopologyHeading is the exact heading text (as it appears, verbatim,
// in CLAUDE.md — composed from .ai/instructions/build-and-test.md) that opens
// the §C container topology table.
const containerTopologyHeading = "## Container topology & network policy"

// checkContainers implements the container-inventory lint (docs/PLAN.md Task
// 37 / FND-18): every Dockerfile*/docker-compose*.y*ml found under roots must
// be a known, documented image lineage.
func checkContainers(roots []string) ([]string, error) {
	documentedImages, err := containerTopologyImages("CLAUDE.md")
	if err != nil {
		return nil, err
	}

	var violations []string
	err = walkFiles(roots, "", func(path string) error {
		base := filepath.Base(path)
		if !containerFilePattern.MatchString(base) {
			return nil
		}
		rel := strings.TrimPrefix(path, "./")
		image, ok := containerFileAllowlist[rel]
		if !ok {
			violations = append(violations, fmt.Sprintf(
				"%s: not listed in containerFileAllowlist (cmd/fitlint/main.go) — untracked container image lineage; add a CLAUDE.md §C table row and an allowlist entry", rel))
			return nil
		}
		if !documentedImages[image] {
			violations = append(violations, fmt.Sprintf(
				"%s: mapped image %s not found in CLAUDE.md's §C container topology table (table row missing or edited)", rel, image))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(violations)
	return violations, nil
}

// containerTopologyImages parses the "Image" column of CLAUDE.md's §C
// container topology table between its heading and the next "## " heading,
// returning the set of exact cell values it documents.
func containerTopologyImages(claudeMDPath string) (map[string]bool, error) {
	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", claudeMDPath, err)
	}
	images := map[string]bool{}
	sawHeading := false
	inTable := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !sawHeading {
			if trimmed == containerTopologyHeading {
				sawHeading = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			break // next section: table is over
		}
		if !strings.HasPrefix(trimmed, "|") {
			if inTable {
				break // blank/prose line after the table body: done
			}
			continue
		}
		inTable = true
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) == 0 {
			continue
		}
		first := strings.TrimSpace(cells[0])
		if first == "Image" || strings.Trim(first, "- ") == "" {
			continue // header row, or the "| --- |" separator row
		}
		images[first] = true
	}
	if !sawHeading {
		return nil, fmt.Errorf("%s: heading %q not found", claudeMDPath, containerTopologyHeading)
	}
	return images, nil
}

// missionLoopFuncPattern matches the "func MissionLoop(" declaration line
// internal/mission/workflow.go's MissionLoop Temporal workflow function
// starts with.
var missionLoopFuncPattern = regexp.MustCompile(`^func MissionLoop\(`)

// requiredLoopContractCall is the activity-name identifier
// internal/mission/workflow.go's MissionLoop must reference before doing
// anything else -- the runtime proof (docs/PLAN.md Task 40 Step 5,
// mission-contract.md §3) that "every loop MUST register a loop contract"
// is enforced as a hard refusal-to-start, not merely documented.
const requiredLoopContractCall = "ActivityRequireLoopContract"

// receiptWrappedMissionActivities are the state-mutating Activities methods
// docs/PLAN.md Task 122 requires be wrapped in kernel.WithReceipt, so a
// commit-then-retry produces exactly one audit row. Listing them here makes the
// gap impossible to silently reopen: adding a new state-mutating activity means
// adding it to this list (and wrapping it) or failing `make fitness`.
var receiptWrappedMissionActivities = []string{
	"AppendMissionTransition",
	"RecordMissionState",
	"RecordGateEvent",
	"ResolveGateEvent",
}

// missionActivityMethodPattern matches a "func (a *Activities) Name(" method
// declaration on internal/mission's Activities type.
var missionActivityMethodPattern = regexp.MustCompile(`^func \(a \*Activities\) ([A-Za-z0-9_]+)\(`)

// checkMissionLoopContract implements docs/PLAN.md Task 40's fitness rule:
// MissionLoop refuses to start without a registered loop contract. This is
// a structural proof, not a runtime one -- it parses MissionLoop's own
// function body (from its "func MissionLoop(" line up to the next
// top-level "func " declaration) and flags any file where that body does
// not reference requiredLoopContractCall, so the refusal-to-start check
// cannot be silently deleted from workflow.go without failing `make
// fitness`. Mirrors checkTerm/checkContract's own text-scan style rather
// than a full go/ast walk, since the property being proven ("this
// identifier appears in this function's source text") does not need one.
func checkMissionLoopContract(roots []string) ([]string, error) {
	var violations []string
	err := walkFiles(roots, ".go", func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		lines := strings.Split(string(data), "\n")

		start := -1
		for i, line := range lines {
			if missionLoopFuncPattern.MatchString(line) {
				start = i
				break
			}
		}
		if start == -1 {
			return nil // this file declares no MissionLoop function
		}

		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "func ") {
				end = i
				break
			}
		}

		body := strings.Join(lines[start:end], "\n")
		if !strings.Contains(body, requiredLoopContractCall) {
			violations = append(violations, fmt.Sprintf(
				"%s:%d: MissionLoop does not reference %s -- it must refuse to start without a registered loop contract (mission-contract.md §3)",
				path, start+1, requiredLoopContractCall))
		}
		return nil
	})
	if err != nil {
		return violations, err
	}

	// docs/PLAN.md Task 122: every state-mutating mission Activities method must
	// be receipt-wrapped (kernel.WithReceipt), so the crash/retry idempotency
	// gap cannot silently reopen. Scan internal/mission/activities.go for each
	// required method and assert its body references kernel.WithReceipt.
	receiptViolations, rerr := checkMissionActivityReceipts(roots)
	if rerr != nil {
		return violations, rerr
	}
	violations = append(violations, receiptViolations...)
	sort.Strings(violations)
	return violations, nil
}

// checkMissionActivityReceipts asserts each of receiptWrappedMissionActivities
// is wrapped in kernel.WithReceipt in the file that defines it, and that any
// *Activities method using the deterministic-write convention (a *WithID store
// call) is likewise wrapped -- so a newly-added state-mutating activity that
// forgets the wrapper fails `make fitness` (docs/PLAN.md Task 122).
func checkMissionActivityReceipts(roots []string) ([]string, error) {
	var violations []string
	err := walkFiles(roots, ".go", func(path string) error {
		if !strings.HasSuffix(filepath.ToSlash(path), "internal/mission/activities.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		lines := strings.Split(string(data), "\n")

		// Index each *Activities method to its body.
		type methodSpan struct {
			name       string
			start, end int
		}
		var methods []methodSpan
		for i, line := range lines {
			if m := missionActivityMethodPattern.FindStringSubmatch(line); m != nil {
				methods = append(methods, methodSpan{name: m[1], start: i})
			}
		}
		for i := range methods {
			methods[i].end = len(lines)
			for j := methods[i].start + 1; j < len(lines); j++ {
				if strings.HasPrefix(lines[j], "func ") {
					methods[i].end = j
					break
				}
			}
		}
		bodyOf := func(name string) (string, bool) {
			for _, ms := range methods {
				if ms.name == name {
					return strings.Join(lines[ms.start:ms.end], "\n"), true
				}
			}
			return "", false
		}

		// Required named activities must be present and receipt-wrapped.
		for _, name := range receiptWrappedMissionActivities {
			body, ok := bodyOf(name)
			if !ok {
				violations = append(violations, fmt.Sprintf(
					"%s: required state-mutating activity %s is missing -- Task 122 receipt-wrapping cannot be verified", path, name))
				continue
			}
			if !strings.Contains(body, "kernel.WithReceipt") {
				violations = append(violations, fmt.Sprintf(
					"%s: activity %s is not wrapped in kernel.WithReceipt -- every state-mutating mission activity must be receipt-keyed (docs/PLAN.md Task 122, C9)", path, name))
			}
		}
		// Heuristic guard against a newly-added state-mutating activity: any
		// method that calls a deterministic-write store method (*WithID) must
		// also be receipt-wrapped.
		for _, ms := range methods {
			body := strings.Join(lines[ms.start:ms.end], "\n")
			if strings.Contains(body, "WithID(") && !strings.Contains(body, "kernel.WithReceipt") {
				violations = append(violations, fmt.Sprintf(
					"%s: activity %s performs a deterministic state write but is not wrapped in kernel.WithReceipt (docs/PLAN.md Task 122)", path, ms.name))
			}
		}
		return nil
	})
	return violations, err
}

// checkResearchBoundary enforces docs/PLAN.md Task 101 (OPP-02) separation of
// duties: internal/opportunity/research proposes and summarizes evidence but
// may never call opportunity.Score or opportunity.Decide — research proposes,
// the kernel gate decides (Constitution C23). research legitimately imports
// the opportunity package for its types, so this is a call-graph prohibition
// (like scripts/check_pec_boundary.sh's precedent), not an import ban: a call
// to the opportunity import's Score/Decide is the violation.
func checkResearchBoundary(roots []string) ([]string, error) {
	const researchPathSegment = "internal/opportunity/research"
	const opportunityImportSuffix = "internal/opportunity"
	prohibited := map[string]bool{"Score": true, "Decide": true}

	var violations []string
	err := walkFiles(roots, ".go", func(path string) error {
		norm := filepath.ToSlash(path)
		if !strings.Contains(norm, researchPathSegment) {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("research-boundary: parse %s: %w", path, perr)
		}

		// Resolve the local name the opportunity package is imported under,
		// and reject a dot-import (which would make Score/Decide unqualified
		// and hide the boundary).
		localName := ""
		for _, imp := range file.Imports {
			ipath := strings.Trim(imp.Path.Value, `"`)
			// Exact opportunity package, not the research subpackage itself.
			if ipath == opportunityImportSuffix || strings.HasSuffix(ipath, "/"+opportunityImportSuffix) {
				if imp.Name != nil {
					if imp.Name.Name == "." {
						violations = append(violations, fmt.Sprintf(
							"%s: dot-imports %s -- research must import it qualified so the Score/Decide prohibition is checkable (C23)",
							norm, opportunityImportSuffix))
						continue
					}
					if imp.Name.Name == "_" {
						continue
					}
					localName = imp.Name.Name
				} else {
					localName = "opportunity"
				}
			}
		}
		if localName == "" {
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != localName {
				return true
			}
			if prohibited[sel.Sel.Name] {
				pos := fset.Position(sel.Pos())
				violations = append(violations, fmt.Sprintf(
					"%s:%d: research references %s.%s -- research proposes, it must never Score or Decide (Constitution C23)",
					norm, pos.Line, localName, sel.Sel.Name))
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(violations)
	return violations, nil
}

// checkPlanValidation enforces docs/PLAN.md Task 104 (SKP-11R2) at CI time:
// every executable PLAN file's task must declare at least one validation
// command or take the explicit, reasoned validation_optout. It parses each
// executable-plan Markdown file (front matter that declares tasks) with
// plan.ParseBytes, whose validation now rejects a task with neither — so a
// plan task that could otherwise validate-as-PASSED by omission fails CI.
func checkPlanValidation(roots []string) ([]string, error) {
	var violations []string
	err := walkFiles(roots, ".md", func(path string) error {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("plan-validation: read %s: %w", path, rerr)
		}
		// Only inspect executable plans: front-matter documents that declare
		// tasks. Prose docs are skipped.
		if !bytes.HasPrefix(bytes.TrimLeft(raw, "\n"), []byte("---")) {
			return nil
		}
		if !bytes.Contains(raw, []byte("\ntasks:")) {
			return nil
		}
		if _, perr := plan.ParseBytes(raw); perr != nil {
			violations = append(violations, fmt.Sprintf("%s: %v", filepath.ToSlash(path), perr))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(violations)
	return violations, nil
}

// checkPlanTopology validates the task DAG of every PLAN card document and
// executable plan under roots (docs/PLAN.md Task 110 / INT-02) using
// plan.ValidateTopology: no self-deps, cycles, unknown/dangling references,
// wave-order errors, or intra-parallel-wave dependency/path overlaps.
//
// A prose PLAN (Markdown "### Task N" cards with "**Depends:**" fields) has its
// dependency graph extracted from the cards; an executable plan (YAML front
// matter declaring tasks) is parsed with plan.ParseBytes.
func checkPlanTopology(roots []string) ([]string, error) {
	taskHeading := regexp.MustCompile(`(?m)^### Task\s+(\d+)\b`)
	dependsField := regexp.MustCompile(`\*\*Depends:\*\*\s*([0-9,\s]+)`)

	var violations []string
	err := walkFiles(roots, ".md", func(path string) error {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return fmt.Errorf("plan-topology: read %s: %w", path, rerr)
		}
		norm := filepath.ToSlash(path)

		// Executable plan: front matter declaring tasks.
		if bytes.HasPrefix(bytes.TrimLeft(raw, "\n"), []byte("---")) && bytes.Contains(raw, []byte("\ntasks:")) {
			doc, perr := plan.ParseBytes(raw)
			if perr != nil {
				// plan-validation reports parse errors; topology only reports
				// topology, so a parse failure here is skipped (not double-reported).
				return nil
			}
			for _, v := range plan.ValidateTopology(plan.TopologyFromDocument(doc)) {
				violations = append(violations, fmt.Sprintf("%s: %s", norm, v))
			}
			return nil
		}

		// Prose PLAN cards.
		headings := taskHeading.FindAllSubmatchIndex(raw, -1)
		if len(headings) == 0 {
			return nil
		}
		var tasks []plan.TopologyTask
		known := map[string]bool{}
		for _, h := range headings {
			known[string(raw[h[2]:h[3]])] = true
		}
		for i, h := range headings {
			id := string(raw[h[2]:h[3]])
			start := h[1]
			end := len(raw)
			if i+1 < len(headings) {
				end = headings[i+1][0]
			}
			card := raw[start:end]
			var deps []string
			if m := dependsField.FindSubmatch(card); m != nil {
				for _, part := range strings.Split(string(m[1]), ",") {
					part = strings.TrimSpace(part)
					// A prose PLAN spans milestones and trims completed cards,
					// so a dependency on a task without a present card is a
					// known-external reference, not a dangling one. Only edges
					// between present cards are topology-checked (cycles,
					// self-deps, ordering); this keeps the whole-file DAG check
					// meaningful without false "unknown" reports.
					if part != "" && known[part] {
						deps = append(deps, part)
					}
				}
			}
			tasks = append(tasks, plan.TopologyTask{ID: id, DependsOn: deps})
		}
		for _, v := range plan.ValidateTopology(tasks) {
			violations = append(violations, fmt.Sprintf("%s: %s", norm, v))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(violations)
	return violations, nil
}

// checkKernelSubprocess enforces docs/PLAN.md Task 115 (SEC-01): internal/kernel
// must never regain a direct bare-subprocess call. Kernel code runs autonomous
// executor work only through the mandatory sandbox seam (SandboxRunner); a
// direct exec.Command / executor.RunSubprocess* in internal/kernel is a
// fail-open host-execution path and fails CI, not just review. Test files are
// exempt (they may spawn helper processes).
func checkKernelSubprocess(roots []string) ([]string, error) {
	const kernelSegment = "internal/kernel/"
	prohibited := map[string]bool{
		"RunSubprocess":          true,
		"RunSubprocessWithStdin": true,
		"Command":                true, // exec.Command
		"CommandContext":         true, // exec.CommandContext
	}
	var violations []string
	err := walkFiles(roots, ".go", func(path string) error {
		norm := filepath.ToSlash(path)
		if !strings.Contains(norm, kernelSegment) {
			return nil
		}
		if strings.HasSuffix(norm, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("subprocess: parse %s: %w", path, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			// Flag exec.Command/CommandContext and <executor>.RunSubprocess*.
			if (ident.Name == "exec" && prohibited[sel.Sel.Name]) ||
				(sel.Sel.Name == "RunSubprocess" || sel.Sel.Name == "RunSubprocessWithStdin") {
				pos := fset.Position(sel.Pos())
				violations = append(violations, fmt.Sprintf(
					"%s:%d: internal/kernel calls %s.%s -- kernel must run executors only through the mandatory sandbox seam, never a bare host subprocess (Task 115, C4/C24)",
					norm, pos.Line, ident.Name, sel.Sel.Name))
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(violations)
	return violations, nil
}

// checkExecutorEnv enforces docs/PLAN.md Task 117 (SEC-03): no process-global
// credential mutation on the executor/kernel path. os.Setenv/os.Unsetenv under
// internal/executor/** or internal/kernel/** is a concurrency hazard — a
// credential installed for one task can leak to or be clobbered by another
// running concurrently in the same process. Credentials must reach a child
// process through its own per-invocation environment (RunSubprocessWithEnv),
// never the shared daemon process. Test files are exempt by an explicit
// allowance (they may set process env to arrange a scenario).
func checkExecutorEnv(roots []string) ([]string, error) {
	segments := []string{"internal/executor/", "internal/kernel/"}
	var violations []string
	err := walkFiles(roots, ".go", func(path string) error {
		norm := filepath.ToSlash(path)
		inScope := false
		for _, seg := range segments {
			if strings.Contains(norm, seg) {
				inScope = true
				break
			}
		}
		if !inScope || strings.HasSuffix(norm, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("env: parse %s: %w", path, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "os" {
				return true
			}
			if sel.Sel.Name == "Setenv" || sel.Sel.Name == "Unsetenv" {
				pos := fset.Position(sel.Pos())
				violations = append(violations, fmt.Sprintf(
					"%s:%d: os.%s under an executor/kernel path -- credentials must be passed per-invocation (RunSubprocessWithEnv), never via process-global env (Task 117, OWASP A02/A07)",
					norm, pos.Line, sel.Sel.Name))
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(violations)
	return violations, nil
}
