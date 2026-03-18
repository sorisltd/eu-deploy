package analytics

import (
	"fmt"
	"strings"
	"time"
)

const SchemaSQL = `
CREATE TABLE IF NOT EXISTS analytics_projects (
  id           BIGSERIAL PRIMARY KEY,
  project_name TEXT NOT NULL UNIQUE,
  hostname     TEXT,
  log_path     TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS analytics_buffer (
  id            BIGSERIAL PRIMARY KEY,
  project_id    BIGINT NOT NULL REFERENCES analytics_projects(id) ON DELETE CASCADE,
  timestamp     TIMESTAMPTZ NOT NULL,
  ip_hash       CHAR(64) NOT NULL,
  country_code  CHAR(2) NOT NULL DEFAULT 'ZZ',
  continent     CHAR(2) NOT NULL DEFAULT 'UN',
  asn           BIGINT,
  isp_name      TEXT,
  path          TEXT,
  status_code   INTEGER,
  bytes_sent    BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS analytics_buffer_project_timestamp_idx
  ON analytics_buffer(project_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS analytics_buffer_timestamp_idx
  ON analytics_buffer(timestamp DESC);

CREATE TABLE IF NOT EXISTS analytics_daily (
  id            BIGSERIAL PRIMARY KEY,
  project_id    BIGINT NOT NULL REFERENCES analytics_projects(id) ON DELETE CASCADE,
  date          DATE NOT NULL,
  country_code  CHAR(2) NOT NULL DEFAULT 'ZZ',
  continent     CHAR(2) NOT NULL DEFAULT 'UN',
  asn           BIGINT,
  isp_name      TEXT,
  visitors      INTEGER NOT NULL DEFAULT 0,
  requests      INTEGER NOT NULL DEFAULT 0,
  bandwidth_mb  NUMERIC(14,2) NOT NULL DEFAULT 0,
  UNIQUE(project_id, date, country_code, continent, asn, isp_name)
);

CREATE INDEX IF NOT EXISTS analytics_daily_project_date_idx
  ON analytics_daily(project_id, date DESC);

CREATE INDEX IF NOT EXISTS analytics_daily_continent_date_idx
  ON analytics_daily(continent, date DESC);

CREATE TABLE IF NOT EXISTS analytics_processor_state (
  log_path     TEXT PRIMARY KEY,
  inode        BIGINT NOT NULL DEFAULT 0,
  byte_offset  BIGINT NOT NULL DEFAULT 0,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

func AggregateSQL(targetDate time.Time) string {
	dayLiteral := targetDate.UTC().Format("2006-01-02")

	return strings.TrimSpace(fmt.Sprintf(`
WITH daily_rows AS (
  SELECT
    project_id,
    DATE(timezone('UTC', timestamp)) AS date,
    country_code,
    continent,
    asn,
    isp_name,
    COUNT(DISTINCT ip_hash)::INTEGER AS visitors,
    COUNT(*)::INTEGER AS requests,
    ROUND(SUM(bytes_sent)::NUMERIC / 1048576.0, 2) AS bandwidth_mb
  FROM analytics_buffer
  WHERE DATE(timezone('UTC', timestamp)) = DATE '%s'
  GROUP BY project_id, DATE(timezone('UTC', timestamp)), country_code, continent, asn, isp_name
)
INSERT INTO analytics_daily (
  project_id,
  date,
  country_code,
  continent,
  asn,
  isp_name,
  visitors,
  requests,
  bandwidth_mb
)
SELECT
  project_id,
  date,
  country_code,
  continent,
  asn,
  isp_name,
  visitors,
  requests,
  bandwidth_mb
FROM daily_rows
ON CONFLICT (project_id, date, country_code, continent, asn, isp_name)
DO UPDATE SET
  visitors = EXCLUDED.visitors,
  requests = EXCLUDED.requests,
  bandwidth_mb = EXCLUDED.bandwidth_mb;

DELETE FROM analytics_buffer
WHERE timestamp < timezone('UTC', NOW()) - INTERVAL '48 hours';
`, dayLiteral))
}
