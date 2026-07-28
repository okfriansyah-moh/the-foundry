package detect

import (
	"regexp"
	"sort"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/plan"
)

var hostPattern = regexp.MustCompile(`https?://([a-zA-Z0-9._-]+)`)

func FromDocument(doc *plan.Document) []plan.Effect {
	if doc == nil {
		return nil
	}
	seen := map[string]plan.Effect{}
	add := func(kind plan.EffectKind, target string) {
		key := string(kind) + "|" + target
		seen[key] = plan.Effect{Kind: kind, Target: target}
	}

	for _, task := range doc.Tasks {
		for _, file := range task.Files {
			lower := strings.ToLower(file)
			switch {
			case strings.HasSuffix(lower, "go.mod"),
				strings.HasSuffix(lower, "go.sum"),
				strings.HasSuffix(lower, "package.json"),
				strings.HasSuffix(lower, "package-lock.json"),
				strings.HasSuffix(lower, "pnpm-lock.yaml"),
				strings.HasSuffix(lower, "yarn.lock"):
				add(plan.EffectDependency, file)
			case strings.Contains(lower, "migration"):
				add(plan.EffectMigration, file)
			case strings.Contains(lower, "billing") || strings.Contains(lower, "stripe") || strings.Contains(lower, "payment"):
				add(plan.EffectBilling, file)
			case strings.Contains(lower, "deploy") || strings.Contains(lower, "fly.toml"):
				add(plan.EffectDeploy, file)
			case strings.Contains(lower, "secret") || strings.Contains(lower, ".env"):
				add(plan.EffectSecret, file)
			case strings.Contains(lower, "permission") || strings.Contains(lower, "authz") || strings.Contains(lower, "rbac"):
				add(plan.EffectPermission, file)
			}
			if strings.Contains(lower, "drop") || strings.Contains(lower, "truncate") {
				add(plan.EffectDestructive, file)
			}
		}

		for _, cmd := range append(task.Commands, task.ValidationCommands...) {
			lower := strings.ToLower(cmd)
			if strings.Contains(lower, "drop table") || strings.Contains(lower, "truncate ") {
				add(plan.EffectDestructive, cmd)
			}
			if strings.Contains(lower, "stripe") || strings.Contains(lower, "billing") || strings.Contains(lower, "payment") {
				add(plan.EffectBilling, cmd)
			}
			if strings.Contains(lower, "flyctl") || strings.Contains(lower, "vercel") || strings.Contains(lower, "deploy") {
				add(plan.EffectDeploy, cmd)
			}
			matches := hostPattern.FindAllStringSubmatch(cmd, -1)
			for _, m := range matches {
				if len(m) > 1 {
					add(plan.EffectNetwork, m[1])
				}
			}
		}
	}

	out := make([]plan.Effect, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Target < out[j].Target
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
