package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	agentruntime "github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime"
	"github.com/okfriansyah-moh/the-foundry/adapters/agent-runtime/claudecode"
	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
	"github.com/okfriansyah-moh/the-foundry/internal/operatorcfg"
	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
)

func runAgents(args []string) error {
	return runPackageCatalog("agents", args, os.Stdout)
}

func runSkills(args []string) error {
	if len(args) > 0 && args[0] == "rollback" {
		return runSkillRollback(args[1:], os.Stdout)
	}
	return runPackageCatalog("skills", args, os.Stdout)
}

func runSkillRollback(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skills rollback", flag.ContinueOnError)
	root := fs.String("root", ".", "Foundry catalog root")
	skillID := fs.String("skill", "", "cataloged skill to roll back")
	pgDSN := fs.String("pg-dsn", "", "Postgres DSN for DB-backed config + durable freeze propagation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *skillID == "" {
		return fmt.Errorf("skills rollback: usage: foundry skills rollback -root path -skill name")
	}
	bridge := &evolve.SkillPackageBridge{Root: *root}
	var db *sql.DB
	if *pgDSN != "" {
		var err error
		db, err = sql.Open("pgx", *pgDSN)
		if err != nil {
			return fmt.Errorf("skills rollback: open postgres: %w", err)
		}
		defer func() { _ = db.Close() }()
		bridge.DurableFreeze = evolve.NewFreezeStore(db)
		bridge.ConfigSource = dbPackageConfigSource{store: operatorcfg.NewStore(db)}
	}
	record, err := bridge.Rollback(context.Background(), *skillID)
	if err != nil {
		return fmt.Errorf("skills rollback: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "skill %s rolled back as immutable v%d\n", record.SkillID, record.ToVersion)
	return err
}

func runPackageCatalog(kind string, args []string, stdout io.Writer) error {
	if len(args) == 0 || !packageSubcommand(args[0]) {
		return fmt.Errorf("%s: usage: foundry %s <list|validate|install|doctor> [-root path] [-enabled path] [-workspace path] [-provider claude-code] [-pg-dsn dsn]", kind, kind)
	}
	fs := flag.NewFlagSet(kind+" "+args[0], flag.ContinueOnError)
	root := fs.String("root", ".", "Foundry catalog root")
	enabledPath := fs.String("enabled", "", "product enabled.yaml (defaults to template declaration)")
	personalProfile := fs.String("personal-profile", "", "personal profile config")
	organizationProfile := fs.String("organization-profile", "", "organization profile config")
	workspace := fs.String("workspace", ".", "product workspace to materialize")
	provider := fs.String("provider", "claude-code", "agent runtime provider")
	pgDSN := fs.String("pg-dsn", "", "Postgres DSN for DB-backed packaging config SoT")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%s %s: unexpected arguments: %v", kind, args[0], fs.Args())
	}
	paths := resolveCatalogPaths(*root, *enabledPath, *personalProfile, *organizationProfile)
	catalogs, enabled, err := loadAndValidateCatalogs(context.Background(), paths, *pgDSN)
	if err != nil {
		return fmt.Errorf("%s %s: %w", kind, args[0], err)
	}
	if args[0] == "install" || args[0] == "doctor" {
		return runPackageMaterialization(context.Background(), stdout, args[0], kind, *provider, *workspace, paths.root, catalogs, enabled)
	}
	if args[0] == "validate" {
		if kind == "agents" {
			_, err = fmt.Fprintf(stdout, "agents catalog valid: %d cataloged, %d enabled\n", len(catalogs.Agents.Agents), len(enabled.Agents))
		} else {
			_, err = fmt.Fprintf(stdout, "skills catalog valid: %d core, %d domain, %d core enabled, %d domain enabled\n", len(catalogs.Skills.Skills), len(catalogs.Skills.DomainSkills), len(enabled.Skills), len(enabled.DomainSkills))
		}
		return err
	}
	return writeCatalogList(stdout, kind, catalogs, enabled)
}

func packageSubcommand(command string) bool {
	return command == "list" || command == "validate" || command == "install" || command == "doctor"
}

func runPackageMaterialization(ctx context.Context, stdout io.Writer, command, kind, provider, workspace, root string, catalogs packaging.Catalogs, enabled packaging.Enablement) error {
	var installKind agentruntime.Kind
	switch kind {
	case "agents":
		installKind = agentruntime.KindAgents
	case "skills":
		installKind = agentruntime.KindSkills
	default:
		return fmt.Errorf("unsupported package kind %q", kind)
	}
	var materializer agentruntime.Materializer
	switch provider {
	case "claude-code":
		materializer = claudecode.Materializer{}
	default:
		return fmt.Errorf("%s %s: unsupported provider %q", kind, command, provider)
	}
	var (
		result agentruntime.Result
		err    error
	)
	if command == "install" {
		result, err = packaging.Install(ctx, root, workspace, catalogs, enabled, installKind, materializer)
	} else {
		result, err = packaging.Doctor(ctx, root, workspace, catalogs, enabled, installKind, materializer)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", kind, command, err)
	}
	_, err = fmt.Fprintf(stdout, "%s %s %s: %d files, manifest %s\n", kind, command, result.Provider, result.Files, result.ManifestDigest)
	return err
}

type catalogPaths struct {
	root                string
	enabled             string
	personalProfile     string
	organizationProfile string
}

func resolveCatalogPaths(root, enabled, personal, organization string) catalogPaths {
	if enabled == "" {
		enabled = filepath.Join(root, "templates/product/.foundry/skills/enabled.yaml")
	}
	if personal == "" {
		personal = filepath.Join(root, "config/profiles/personal-autonomous-venture.yaml")
	}
	if organization == "" {
		organization = filepath.Join(root, "config/profiles/organization-10x.yaml")
	}
	return catalogPaths{root: root, enabled: enabled, personalProfile: personal, organizationProfile: organization}
}

func loadAndValidateCatalogs(ctx context.Context, paths catalogPaths, pgDSN string) (packaging.Catalogs, packaging.Enablement, error) {
	if pgDSN != "" {
		db, err := sql.Open("pgx", pgDSN)
		if err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, fmt.Errorf("open postgres: %w", err)
		}
		defer func() { _ = db.Close() }()
		store := operatorcfg.NewStore(db)
		catalogs, err := store.LoadCatalogs(ctx)
		if err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, fmt.Errorf("load catalogs from db: %w", err)
		}
		enabled, err := store.LoadEnablement(ctx, "personal-autonomous-venture")
		if err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, fmt.Errorf("load enablement from db: %w", err)
		}
		personal, err := store.LoadProfileEnablement(ctx, "personal-autonomous-venture")
		if err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, fmt.Errorf("load personal profile packages from db: %w", err)
		}
		organization, err := store.LoadProfileEnablement(ctx, "organization-10x")
		if err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, fmt.Errorf("load organization profile packages from db: %w", err)
		}
		if err := packaging.ValidateCatalogs(paths.root, catalogs); err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, err
		}
		if err := packaging.ValidateEnablement(catalogs, enabled); err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, err
		}
		if err := packaging.ValidateProfiles(catalogs, personal, organization); err != nil {
			return packaging.Catalogs{}, packaging.Enablement{}, err
		}
		return catalogs, enabled, nil
	}

	catalogs, err := packaging.LoadCatalogs(paths.root)
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, err
	}
	if err := packaging.ValidateCatalogs(paths.root, catalogs); err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, err
	}
	enabled, err := packaging.LoadEnablement(paths.enabled)
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, err
	}
	if err := packaging.ValidateEnablement(catalogs, enabled); err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, err
	}
	personal, err := packaging.LoadProfileEnablement(paths.personalProfile)
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, err
	}
	organization, err := packaging.LoadProfileEnablement(paths.organizationProfile)
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, err
	}
	if err := packaging.ValidateProfiles(catalogs, personal, organization); err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, err
	}
	return catalogs, enabled, nil
}

func writeCatalogList(w io.Writer, kind string, catalogs packaging.Catalogs, enabled packaging.Enablement) error {
	if _, err := fmt.Fprintln(w, "NAME\tTYPE\tENABLED\tDESCRIPTION"); err != nil {
		return err
	}
	enabledNames := make(map[string]struct{})
	var rows []catalogRow
	if kind == "agents" {
		for _, name := range enabled.Agents {
			enabledNames[name] = struct{}{}
		}
		for _, agent := range catalogs.Agents.Agents {
			rows = append(rows, catalogRow{name: agent.Name, kind: "agent", description: agent.Description})
		}
	} else {
		for _, name := range append(append([]string(nil), enabled.Skills...), enabled.DomainSkills...) {
			enabledNames[name] = struct{}{}
		}
		for _, skill := range catalogs.Skills.Skills {
			rows = append(rows, catalogRow{name: skill.Name, kind: "skill", description: skill.Description})
		}
		for _, skill := range catalogs.Skills.DomainSkills {
			rows = append(rows, catalogRow{name: skill.Name, kind: "domain-skill", description: skill.Description})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, row := range rows {
		_, isEnabled := enabledNames[row.name]
		if _, err := fmt.Fprintf(w, "%s\t%s\t%t\t%s\n", row.name, row.kind, isEnabled, row.description); err != nil {
			return err
		}
	}
	return nil
}

type catalogRow struct {
	name        string
	kind        string
	description string
}

type dbPackageConfigSource struct {
	store *operatorcfg.Store
}

func (s dbPackageConfigSource) Load(ctx context.Context) (packaging.Catalogs, packaging.Enablement, packaging.ProfileEnablement, packaging.ProfileEnablement, error) {
	catalogs, err := s.store.LoadCatalogs(ctx)
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, err
	}
	enabled, err := s.store.LoadEnablement(ctx, "personal-autonomous-venture")
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, err
	}
	personal, err := s.store.LoadProfileEnablement(ctx, "personal-autonomous-venture")
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, err
	}
	organization, err := s.store.LoadProfileEnablement(ctx, "organization-10x")
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, err
	}
	return catalogs, enabled, personal, organization, nil
}
