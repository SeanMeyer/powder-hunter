package config

import (
	"strconv"
	"time"
)

// PipelineConfig holds configuration for the storm-tracking pipeline loop.
// Values are typically sourced from environment variables (Docker) or CLI flags.
type PipelineConfig struct {
	DBPath       string
	LoopInterval time.Duration
	DryRun       bool
	Budget       float64
	RegionFilter string
	Verbose      bool
}

// PipelineConfigFromEnv builds a PipelineConfig by reading values through the
// supplied lookup function. Passing os.Getenv as lookup reads real environment
// variables; tests can inject a map-backed function instead.
func PipelineConfigFromEnv(lookup func(string) string) PipelineConfig {
	cfg := PipelineConfig{
		DBPath:       "/data/powder-hunter.db",
		LoopInterval: 12 * time.Hour,
		DryRun:       false,
		Budget:       20.0,
		RegionFilter: "",
		Verbose:      false,
	}

	if v := lookup("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := lookup("LOOP_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.LoopInterval = d
		}
	}
	if v := lookup("DRY_RUN"); v == "true" {
		cfg.DryRun = true
	}
	if v := lookup("BUDGET"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Budget = f
		}
	}
	if v := lookup("REGION_FILTER"); v != "" {
		cfg.RegionFilter = v
	}
	if v := lookup("VERBOSE"); v == "true" {
		cfg.Verbose = true
	}

	return cfg
}
