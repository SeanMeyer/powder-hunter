package config

import (
	"testing"
	"time"
)

func lookupFromMap(m map[string]string) func(string) string {
	return func(key string) string {
		return m[key]
	}
}

func TestPipelineConfigFromEnv_Defaults(t *testing.T) {
	cfg := PipelineConfigFromEnv(lookupFromMap(map[string]string{}))

	if cfg.DBPath != "/data/powder-hunter.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "/data/powder-hunter.db")
	}
	if cfg.LoopInterval != 12*time.Hour {
		t.Errorf("LoopInterval: got %v, want %v", cfg.LoopInterval, 12*time.Hour)
	}
	if cfg.DryRun != false {
		t.Errorf("DryRun: got %v, want false", cfg.DryRun)
	}
	if cfg.Budget != 20.0 {
		t.Errorf("Budget: got %v, want 20.0", cfg.Budget)
	}
	if cfg.RegionFilter != "" {
		t.Errorf("RegionFilter: got %q, want empty", cfg.RegionFilter)
	}
	if cfg.Verbose != false {
		t.Errorf("Verbose: got %v, want false", cfg.Verbose)
	}
}

func TestPipelineConfigFromEnv_AllOverrides(t *testing.T) {
	env := map[string]string{
		"DB_PATH":       "/tmp/test.db",
		"LOOP_INTERVAL": "30m",
		"DRY_RUN":       "true",
		"BUDGET":        "5.50",
		"REGION_FILTER":  "CO",
		"VERBOSE":       "true",
	}
	cfg := PipelineConfigFromEnv(lookupFromMap(env))

	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath: got %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.LoopInterval != 30*time.Minute {
		t.Errorf("LoopInterval: got %v, want %v", cfg.LoopInterval, 30*time.Minute)
	}
	if cfg.DryRun != true {
		t.Errorf("DryRun: got %v, want true", cfg.DryRun)
	}
	if cfg.Budget != 5.50 {
		t.Errorf("Budget: got %v, want 5.50", cfg.Budget)
	}
	if cfg.RegionFilter != "CO" {
		t.Errorf("RegionFilter: got %q, want %q", cfg.RegionFilter, "CO")
	}
	if cfg.Verbose != true {
		t.Errorf("Verbose: got %v, want true", cfg.Verbose)
	}
}

func TestPipelineConfigFromEnv_InvalidValuesUseDefaults(t *testing.T) {
	env := map[string]string{
		"LOOP_INTERVAL": "not-a-duration",
		"BUDGET":        "not-a-number",
		"DRY_RUN":       "yes",
		"VERBOSE":       "1",
	}
	cfg := PipelineConfigFromEnv(lookupFromMap(env))

	if cfg.LoopInterval != 12*time.Hour {
		t.Errorf("LoopInterval: got %v, want %v (default on bad input)", cfg.LoopInterval, 12*time.Hour)
	}
	if cfg.Budget != 20.0 {
		t.Errorf("Budget: got %v, want 20.0 (default on bad input)", cfg.Budget)
	}
	if cfg.DryRun != false {
		t.Errorf("DryRun: got %v, want false (only 'true' string enables)", cfg.DryRun)
	}
	if cfg.Verbose != false {
		t.Errorf("Verbose: got %v, want false (only 'true' string enables)", cfg.Verbose)
	}
}
