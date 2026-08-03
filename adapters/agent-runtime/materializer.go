package agentruntime

import "context"

// Kind identifies the independently installable catalog projection.
type Kind string

const (
	KindAgents Kind = "agents"
	KindSkills Kind = "skills"
)

// PackageSet contains validated canonical packages for one install kind.
type PackageSet struct {
	Kind             Kind
	CatalogDigest    string
	EnablementDigest string
	Packages         []Package
}

// Package is one enabled canonical package. Source is the primary agent or
// SKILL.md body; References are package-relative supporting files.
type Package struct {
	Name        string
	Description string
	Skills      []string
	Domain      bool
	Source      []byte
	References  []File
}

// File is one package-relative supporting file.
type File struct {
	Path  string
	Bytes []byte
}

// Result reports the deterministic projection written or verified.
type Result struct {
	Provider       string
	Kind           Kind
	Files          int
	ManifestDigest string
}

// Materializer projects canonical packages into one provider's workspace
// format and verifies that projection against the same canonical inputs.
type Materializer interface {
	Install(context.Context, string, PackageSet) (Result, error)
	Doctor(context.Context, string, PackageSet) (Result, error)
}
