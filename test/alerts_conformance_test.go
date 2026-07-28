package recoverylive_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAlertsConformance(t *testing.T) {
	files := []string{"../deploy/prometheus/rules/foundry.yaml", "../deploy/prometheus/alertmanager/routes.yaml"}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		var v any
		if err := yaml.Unmarshal(raw, &v); err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		if v == nil {
			t.Fatalf("yaml file %s decoded to nil", file)
		}
	}
}
