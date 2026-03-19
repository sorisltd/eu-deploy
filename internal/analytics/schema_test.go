package analytics

import (
	"strings"
	"testing"
	"time"
)

func TestAggregateSQLUsesDateLiteral(t *testing.T) {
	sql := AggregateSQL(time.Date(2026, time.March, 18, 9, 30, 0, 0, time.UTC))

	if !strings.Contains(sql, "DATE '2026-03-18'") {
		t.Fatalf("expected aggregate SQL to include a SQL date literal, got:\n%s", sql)
	}

	if strings.Contains(sql, `DATE "2026-03-18"`) {
		t.Fatalf("expected aggregate SQL not to include a quoted identifier date literal, got:\n%s", sql)
	}

	for _, expected := range []string{
		"human_visitors",
		"human_requests",
		"human_bandwidth_mb",
		"has_known_bot_ua",
		"flags.has_known_bot_ua",
		"short_404_probes >= 2",
		"crawler_utility_requests > 0 AND flags.non_utility_requests = 0",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("expected aggregate SQL to include %q, got:\n%s", expected, sql)
		}
	}
}

func TestSchemaSQLAddsHumanColumns(t *testing.T) {
	for _, expected := range []string{
		"is_known_bot  BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN IF NOT EXISTS is_known_bot BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN IF NOT EXISTS human_visitors",
		"ADD COLUMN IF NOT EXISTS human_requests",
		"ADD COLUMN IF NOT EXISTS human_bandwidth_mb",
	} {
		if !strings.Contains(SchemaSQL, expected) {
			t.Fatalf("expected schema SQL to include %q, got:\n%s", expected, SchemaSQL)
		}
	}
}
