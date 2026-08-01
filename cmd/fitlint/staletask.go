package main

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// futureWorkPatterns mark a comment as deferring work to a later task.
var futureWorkPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)futures?\s+task`),
	regexp.MustCompile(`(?i)pending\s+task`),
	regexp.MustCompile(`(?i)not\s+yet\s+wired`),
	regexp.MustCompile(`(?i)not-yet-built`),
	regexp.MustCompile(`(?i)has\s+not\s+landed`),
	regexp.MustCompile(`(?i)STUB\s+pending`),
	regexp.MustCompile(`(?i)is\s+a\s+STUB\b`),
	regexp.MustCompile(`(?i)whichever\s+future\s+task`),
	regexp.MustCompile(`(?i)will\s+make\s+this`),
	regexp.MustCompile(`(?i)doesn'?t\s+exist\s+yet`),
}

var taskNumberPattern = regexp.MustCompile(`(?i)task\s+(\d+)`)

// checkStaleTaskComments implements docs/PLAN.md Task 131 (DOC-01): a comment
// under internal/ or cmd/ that names a completed Master Index task as future
// work is a violation — the plan and the code must agree about what is done.
func checkStaleTaskComments(roots []string) ([]string, error) {
	completed, err := completedPlanTasks("docs/PLAN.md")
	if err != nil {
		return nil, err
	}

	var violations []string
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
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel := filepath.ToSlash(strings.TrimPrefix(path, "./"))
			scanSeeds := strings.Contains(rel, "test/fitness_seeds/")
			if !scanSeeds && !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
				return nil
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				return fmt.Errorf("stale-task-comment: parse %s: %w", path, perr)
			}
			for _, cg := range file.Comments {
				for _, c := range cg.List {
					text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
					text = strings.TrimPrefix(text, "/*")
					text = strings.TrimSuffix(text, "*/")
					text = strings.TrimSpace(text)
					if !isFutureWorkComment(text) {
						continue
					}
					for _, n := range taskNumbersInComment(text) {
						if completed[n] {
							pos := fset.Position(c.Pos())
							violations = append(violations, fmt.Sprintf(
								"%s:%d: comment treats Task %d as future work but Master Index marks it complete (docs/PLAN.md Task 131)",
								rel, pos.Line, n))
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(violations)
	return violations, nil
}

func isFutureWorkComment(text string) bool {
	for _, pat := range futureWorkPatterns {
		if pat.MatchString(text) {
			return true
		}
	}
	return false
}

func taskNumbersInComment(text string) []int {
	matches := taskNumberPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var out []int
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// completedPlanTasks parses docs/PLAN.md's §D Master Index for checked tasks.
func completedPlanTasks(planPath string) (map[int]bool, error) {
	f, err := os.Open(planPath)
	if err != nil {
		return nil, fmt.Errorf("stale-task-comment: read %s: %w", planPath, err)
	}
	defer func() { _ = f.Close() }()

	completed := map[int]bool{}
	inIndex := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "## D. Master Task Index") {
			inIndex = true
			continue
		}
		if inIndex && strings.HasPrefix(strings.TrimSpace(line), "### D-P") {
			break
		}
		if !inIndex {
			continue
		}
		if !strings.Contains(line, "✅") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) < 3 {
			continue
		}
		numField := strings.TrimSpace(fields[2])
		n, err := strconv.Atoi(numField)
		if err != nil {
			continue
		}
		completed[n] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stale-task-comment: scan %s: %w", planPath, err)
	}
	return completed, nil
}
