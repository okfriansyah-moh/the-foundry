package product

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const TemplateRoot = "templates/product"

type InstantiateOptions struct {
	Name        string
	Destination string
}

func Instantiate(opts InstantiateOptions) (string, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return "", fmt.Errorf("product template: name is required")
	}
	if opts.Destination == "" {
		return "", fmt.Errorf("product template: destination is required")
	}
	target := filepath.Join(opts.Destination, name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("product template: mkdir target: %w", err)
	}

	templateRoot, err := resolveTemplateRoot()
	if err != nil {
		return "", err
	}
	if err := filepath.WalkDir(templateRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(templateRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		destPath := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rendered := strings.ReplaceAll(string(raw), "{{PRODUCT_NAME}}", name)
		return os.WriteFile(destPath, []byte(rendered), 0o644)
	}); err != nil {
		return "", fmt.Errorf("product template: instantiate: %w", err)
	}
	return target, nil
}

func resolveTemplateRoot() (string, error) {
	candidates := []string{
		TemplateRoot,
		filepath.Join("..", "..", TemplateRoot),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("product template: %s not found", TemplateRoot)
}
