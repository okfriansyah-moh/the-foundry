package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// IndexRow is one row of docs/PLAN.md §D Master Task Index.
type IndexRow struct {
	Done     bool
	Task     int
	Alias    string
	Title    string
	Phase    string
	Depends  []int
	Parallel bool
}

// Card is the parsed body of one "### Task N (...)" section.
type Card struct {
	Task       int
	Alias      string
	Title      string
	Risk       string
	Rev        string
	Exec       string
	Outputs    string
	Validation []string
	Body       string
}

// Plan is a parsed docs/PLAN.md (or a scratch fixture with the same shape).
type Plan struct {
	Path  string
	Raw   string
	Index []IndexRow
	Cards map[int]*Card
}

var (
	indexRowPattern     = regexp.MustCompile(`^\|\s*(✅|☐)\s*\|\s*(\d+)\s*\|\s*([^|]*)\|\s*([^|]*)\|\s*([^|]*)\|\s*([^|]*)\|\s*([^|]*)\|\s*$`)
	cardHeadingPattern  = regexp.MustCompile(`(?m)^### Task (\d+) \(([A-Za-z0-9-]+)\)(?: \[P\])? — (.+)$`)
	riskFieldPattern    = regexp.MustCompile(`\*\*Risk:\*\*\s*([A-Za-z]+)`)
	revFieldPattern     = regexp.MustCompile(`\*\*Rev:\*\*\s*\**\s*(R[1-4])`)
	execFieldPattern    = regexp.MustCompile(`\*\*Exec:\*\*\s*([^\s·]+)`)
	outputsFieldPattern = regexp.MustCompile(`(?m)^-\s\*\*Outputs:\*\*\s*(.+)$`)
	validationLinePatt  = regexp.MustCompile(`(?m)^-\s\*\*Validation:\*\*\s*(.+)$`)
	backtickPattern     = regexp.MustCompile("`([^`]+)`")
	statusLinePattern   = regexp.MustCompile(`(?m)^- \*\*Status:\*\* .+$`)
)

// ParsePlan reads and parses a PLAN.md-shaped file (Task 3 Step 1: Parser).
func ParsePlan(path string) (*Plan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan %s: %w", path, err)
	}
	p := &Plan{Path: path, Raw: string(raw)}

	p.Index = parseIndex(p.Raw)
	p.Cards, err = parseCards(p.Raw)
	if err != nil {
		return nil, fmt.Errorf("parse cards in %s: %w", path, err)
	}
	return p, nil
}

func parseIndex(raw string) []IndexRow {
	var rows []IndexRow
	for _, line := range strings.Split(raw, "\n") {
		m := indexRowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		taskNum, err := strconv.Atoi(strings.TrimSpace(m[2]))
		if err != nil {
			continue // header/separator rows ("Task", "---") aren't numeric; skip.
		}
		rows = append(rows, IndexRow{
			Done:     strings.TrimSpace(m[1]) == "✅",
			Task:     taskNum,
			Alias:    strings.TrimSpace(m[3]),
			Title:    strings.TrimSpace(m[4]),
			Phase:    strings.TrimSpace(m[5]),
			Depends:  parseDepends(m[6]),
			Parallel: strings.Contains(m[7], "[P]"),
		})
	}
	return rows
}

func parseDepends(field string) []int {
	field = strings.TrimSpace(field)
	if field == "" || field == "—" || field == "-" {
		return nil
	}
	var deps []int
	for _, part := range strings.Split(field, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue // free-text depends notes are ignored; numeric deps are authoritative.
		}
		deps = append(deps, n)
	}
	return deps
}

func parseCards(raw string) (map[int]*Card, error) {
	matches := cardHeadingPattern.FindAllStringSubmatchIndex(raw, -1)
	cards := make(map[int]*Card, len(matches))
	for i, m := range matches {
		start := m[0]
		end := len(raw)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := raw[start:end]

		taskNum, err := strconv.Atoi(raw[m[2]:m[3]])
		if err != nil {
			return nil, fmt.Errorf("card heading task number: %w", err)
		}
		card := &Card{
			Task:  taskNum,
			Alias: raw[m[4]:m[5]],
			Title: strings.TrimSpace(raw[m[6]:m[7]]),
			Body:  body,
		}
		if fm := riskFieldPattern.FindStringSubmatch(body); fm != nil {
			card.Risk = fm[1]
		}
		if fm := revFieldPattern.FindStringSubmatch(body); fm != nil {
			card.Rev = fm[1]
		}
		if fm := execFieldPattern.FindStringSubmatch(body); fm != nil {
			card.Exec = fm[1]
		}
		if fm := outputsFieldPattern.FindStringSubmatch(body); fm != nil {
			card.Outputs = strings.TrimSpace(fm[1])
		}
		if fm := validationLinePatt.FindStringSubmatch(body); fm != nil {
			for _, bm := range backtickPattern.FindAllStringSubmatch(fm[1], -1) {
				card.Validation = append(card.Validation, bm[1])
			}
		}
		cards[taskNum] = card
	}
	return cards, nil
}

// Eligible returns not-yet-done rows whose every Depends entry is ✅ in the Index
// (Task 3 Step 2), sorted ascending by task number.
func (p *Plan) Eligible() []IndexRow {
	done := make(map[int]bool, len(p.Index))
	for _, row := range p.Index {
		done[row.Task] = row.Done
	}
	var out []IndexRow
	for _, row := range p.Index {
		if row.Done {
			continue
		}
		ok := true
		for _, d := range row.Depends {
			if !done[d] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, row)
		}
	}
	return out
}

// MarkDone flips a task's "- **Status:**" line to "✅ <date>" and its Master Index
// checkbox to ✅, writing the plan file back in place (Task 3 Step 4/manual protocol
// step 5: "flip Status..., check the Index box").
func (p *Plan) MarkDone(taskNum int, date string) error {
	raw := p.Raw
	matches := cardHeadingPattern.FindAllStringSubmatchIndex(raw, -1)

	bodyStart := -1
	bodyEnd := len(raw)
	for i, m := range matches {
		n, err := strconv.Atoi(raw[m[2]:m[3]])
		if err != nil {
			return fmt.Errorf("mark done: card heading task number: %w", err)
		}
		if n == taskNum {
			bodyStart = m[0]
			if i+1 < len(matches) {
				bodyEnd = matches[i+1][0]
			}
			break
		}
	}
	if bodyStart < 0 {
		return fmt.Errorf("mark done: task %d not found", taskNum)
	}

	body := raw[bodyStart:bodyEnd]
	loc := statusLinePattern.FindStringIndex(body)
	if loc == nil {
		return fmt.Errorf("mark done: task %d has no Status line", taskNum)
	}
	newStatus := fmt.Sprintf("- **Status:** ✅ %s", date)
	newBody := body[:loc[0]] + newStatus + body[loc[1]:]
	raw = raw[:bodyStart] + newBody + raw[bodyEnd:]

	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		m := indexRowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(m[2]))
		if err != nil || n != taskNum {
			continue
		}
		lines[i] = strings.Replace(line, "☐", "✅", 1)
		break
	}
	raw = strings.Join(lines, "\n")

	if err := os.WriteFile(p.Path, []byte(raw), 0o644); err != nil {
		return fmt.Errorf("write plan %s: %w", p.Path, err)
	}
	p.Raw = raw

	// Keep the in-memory Index in sync with the file: without this, a live Runner's
	// Eligible() would keep re-selecting a task this call just marked done, spinning
	// forever instead of advancing to the next one.
	for i, row := range p.Index {
		if row.Task == taskNum {
			p.Index[i].Done = true
			break
		}
	}
	return nil
}
