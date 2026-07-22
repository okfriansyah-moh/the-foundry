package profile

import (
	"encoding/json"
	"testing"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		wantErr  bool
		wantPath string // exact ConfigError.Path when wantErr
	}{
		{
			name:   "valid minimal",
			config: `{"schema_version":1,"budget":{"max_usd":100}}`,
		},
		{
			name:   "valid with notify",
			config: `{"schema_version":1,"budget":{"max_usd":50},"notify":{"channels":["slack"]}}`,
		},
		{
			name:     "missing schema_version",
			config:   `{"budget":{"max_usd":100}}`,
			wantErr:  true,
			wantPath: "/config",
		},
		{
			name:     "wrong schema_version",
			config:   `{"schema_version":2,"budget":{"max_usd":100}}`,
			wantErr:  true,
			wantPath: "/config/schema_version",
		},
		{
			name:     "negative budget",
			config:   `{"schema_version":1,"budget":{"max_usd":-1}}`,
			wantErr:  true,
			wantPath: "/config/budget/max_usd",
		},
		{
			name:     "missing budget",
			config:   `{"schema_version":1}`,
			wantErr:  true,
			wantPath: "/config",
		},
		{
			name:     "budget wrong type",
			config:   `{"schema_version":1,"budget":{"max_usd":"lots"}}`,
			wantErr:  true,
			wantPath: "/config/budget/max_usd",
		},
		{
			name:    "unknown field rejected",
			config:  `{"schema_version":1,"budget":{"max_usd":10},"unexpected":"x"}`,
			wantErr: true,
		},
		{
			name:     "not valid json",
			config:   `{not json`,
			wantErr:  true,
			wantPath: "/config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(json.RawMessage(tt.config))
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidateConfig(%s): unexpected error: %v", tt.config, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateConfig(%s): want error, got nil", tt.config)
			}
			ce, ok := err.(*ConfigError)
			if !ok {
				t.Fatalf("ValidateConfig(%s): want *ConfigError, got %T", tt.config, err)
			}
			if tt.wantPath != "" && ce.Path != tt.wantPath {
				t.Fatalf("ValidateConfig(%s): Path = %q, want %q (message: %s)", tt.config, ce.Path, tt.wantPath, ce.Message)
			}
			if ce.Message == "" {
				t.Fatalf("ValidateConfig(%s): empty Message", tt.config)
			}
		})
	}
}
