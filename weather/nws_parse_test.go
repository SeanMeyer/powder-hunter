package weather

import (
	"testing"
	"time"
)

func TestParseISO8601Duration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"1 hour", "PT1H", time.Hour, false},
		{"6 hours", "PT6H", 6 * time.Hour, false},
		{"1 day", "P1D", 24 * time.Hour, false},
		{"1 day 12 hours", "P1DT12H", 36 * time.Hour, false},
		{"30 minutes", "PT30M", 30 * time.Minute, false},
		{"2 weeks", "P2W", 2 * 7 * 24 * time.Hour, false},
		{"45 seconds", "PT45S", 45 * time.Second, false},
		{"mixed day + minutes", "P1DT30M", 24*time.Hour + 30*time.Minute, false},
		{"invalid no P prefix", "INVALID", 0, true},
		{"empty string", "", 0, true},
		{"only P no components", "P", 0, false},
		{"unknown unit", "P1X", 0, true},
		{"T section with unknown unit", "PT1X", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseISO8601Duration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseISO8601Duration(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseISO8601Duration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseISO8601Interval(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantDur  time.Duration
		wantErr  bool
	}{
		{
			name:    "1-hour interval",
			input:   "2026-03-05T06:00:00+00:00/PT1H",
			wantDur: time.Hour,
		},
		{
			name:    "6-hour interval",
			input:   "2026-03-05T00:00:00+00:00/PT6H",
			wantDur: 6 * time.Hour,
		},
		{
			name:    "1-day interval",
			input:   "2026-03-05T12:00:00+00:00/P1D",
			wantDur: 24 * time.Hour,
		},
		{
			name:    "missing slash → error",
			input:   "2026-03-05T06:00:00+00:00",
			wantErr: true,
		},
		{
			name:    "invalid timestamp → error",
			input:   "not-a-time/PT1H",
			wantErr: true,
		},
		{
			name:    "invalid duration → error",
			input:   "2026-03-05T06:00:00+00:00/BAD",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTime, gotDur, err := parseISO8601Interval(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseISO8601Interval(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if gotTime.IsZero() {
				t.Errorf("parseISO8601Interval(%q) returned zero time", tt.input)
			}
			if gotDur != tt.wantDur {
				t.Errorf("parseISO8601Interval(%q) duration = %v, want %v", tt.input, gotDur, tt.wantDur)
			}
		})
	}
}
