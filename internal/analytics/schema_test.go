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
}
