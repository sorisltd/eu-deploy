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
  is_known_bot  BOOLEAN NOT NULL DEFAULT FALSE,
  bytes_sent    BIGINT NOT NULL DEFAULT 0
);

ALTER TABLE analytics_buffer
  ADD COLUMN IF NOT EXISTS is_known_bot BOOLEAN NOT NULL DEFAULT FALSE;

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

ALTER TABLE analytics_daily
  ADD COLUMN IF NOT EXISTS human_visitors INTEGER NOT NULL DEFAULT 0;

ALTER TABLE analytics_daily
  ADD COLUMN IF NOT EXISTS human_requests INTEGER NOT NULL DEFAULT 0;

ALTER TABLE analytics_daily
  ADD COLUMN IF NOT EXISTS human_bandwidth_mb NUMERIC(14,2) NOT NULL DEFAULT 0;

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

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func botProbePathCondition(pathExpr string) string {
	path := fmt.Sprintf("COALESCE(%s, '')", pathExpr)
	clauses := []string{
		fmt.Sprintf("%s ~ '^/\\.'", path),
		fmt.Sprintf("%s ILIKE '/wp-%%'", path),
		fmt.Sprintf("%s ILIKE '/wp/%%'", path),
		fmt.Sprintf("%s ILIKE '/wp-admin%%'", path),
		fmt.Sprintf("%s ILIKE '/wp-content%%'", path),
		fmt.Sprintf("%s ILIKE '/wp-includes%%'", path),
		fmt.Sprintf("%s ILIKE '/wordpress%%'", path),
		fmt.Sprintf("%s ILIKE '/xmlrpc.php%%'", path),
		fmt.Sprintf("%s ILIKE '/phpmyadmin%%'", path),
		fmt.Sprintf("%s ILIKE '/pma%%'", path),
		fmt.Sprintf("%s ILIKE '/adminer%%'", path),
		fmt.Sprintf("%s ILIKE '/cgi-bin%%'", path),
		fmt.Sprintf("%s ILIKE '/boaform%%'", path),
		fmt.Sprintf("%s ILIKE '/HNAP1%%'", path),
		fmt.Sprintf("%s ILIKE '/manager/html%%'", path),
		fmt.Sprintf("%s ILIKE '/server-status%%'", path),
		fmt.Sprintf("%s ILIKE '/vendor/phpunit%%'", path),
		fmt.Sprintf("%s ILIKE '/actuator%%'", path),
		fmt.Sprintf("%s ILIKE '/telescope%%'", path),
		fmt.Sprintf("%s ILIKE '/_profiler%%'", path),
		fmt.Sprintf("%s ILIKE '/autodiscover%%'", path),
		fmt.Sprintf("%s ILIKE '/owa%%'", path),
		fmt.Sprintf("%s ILIKE '/ecp%%'", path),
		fmt.Sprintf("%s ILIKE '/solr%%'", path),
		fmt.Sprintf("%s ILIKE '/jmx-console%%'", path),
		fmt.Sprintf("%s ILIKE '/remote/login%%'", path),
		fmt.Sprintf("%s ILIKE '/api/jsonws%%'", path),
		fmt.Sprintf("%s LIKE '%%.env%%'", path),
		fmt.Sprintf("%s LIKE '%%.git%%'", path),
		fmt.Sprintf("%s LIKE '%%.svn%%'", path),
		fmt.Sprintf("(%s LIKE '%%.php%%' AND %s <> '/index.php')", path, path),
	}
	return "(" + strings.Join(clauses, "\n        OR ") + ")"
}

func short404ProbeCondition(pathExpr, statusExpr string) string {
	path := fmt.Sprintf("COALESCE(%s, '')", pathExpr)
	status := fmt.Sprintf("COALESCE(%s, 0)", statusExpr)
	return fmt.Sprintf("(%s BETWEEN 400 AND 499 AND %s ~ '^/[a-z0-9]{1,4}$')", status, path)
}

func crawlerUtilityPathCondition(pathExpr string) string {
	path := fmt.Sprintf("COALESCE(%s, '')", pathExpr)
	paths := []string{
		"/robots.txt",
		"/sitemap.xml",
		"/ads.txt",
		"/favicon.ico",
		"/favicon-32x32.png",
		"/favicon.svg",
		"/apple-touch-icon.png",
		"/manifest.webmanifest",
		"/site.webmanifest",
	}

	literals := make([]string, 0, len(paths))
	for _, value := range paths {
		literals = append(literals, sqlStringLiteral(value))
	}

	return fmt.Sprintf("%s IN (%s)", path, strings.Join(literals, ", "))
}

func humanClassificationCTE(targetDate time.Time) string {
	dayLiteral := targetDate.UTC().Format("2006-01-02")
	dayExpr := "DATE(timezone('UTC', b.timestamp))"
	pathExpr := "b.path"
	statusExpr := "b.status_code"
	botProbe := botProbePathCondition(pathExpr)
	short404Probe := short404ProbeCondition(pathExpr, statusExpr)
	crawlerUtility := crawlerUtilityPathCondition(pathExpr)

	return strings.TrimSpace(fmt.Sprintf(`
WITH buffer_rows AS (
  SELECT
    b.project_id,
    %s AS date,
    COALESCE(NULLIF(b.country_code, ''), 'ZZ') AS country_code,
    COALESCE(NULLIF(b.continent, ''), 'UN') AS continent,
    b.asn,
    COALESCE(NULLIF(b.isp_name, ''), 'Unknown network') AS isp_name,
    b.ip_hash,
    COALESCE(b.path, '/') AS path,
    COALESCE(b.status_code, 0) AS status_code,
    COALESCE(b.is_known_bot, FALSE) AS is_known_bot,
    b.bytes_sent
  FROM analytics_buffer b
  WHERE %s = DATE '%s'
),
ip_day_flags AS (
  SELECT
    project_id,
    date,
    ip_hash,
    BOOL_OR(is_known_bot) AS has_known_bot_ua,
    BOOL_OR(%s) AS has_probe_path,
    COUNT(*) FILTER (WHERE %s) AS short_404_probes,
    COUNT(*) FILTER (WHERE %s) AS crawler_utility_requests,
    COUNT(*) FILTER (WHERE NOT (%s)) AS non_utility_requests
  FROM buffer_rows b
  GROUP BY project_id, date, ip_hash
),
classified_rows AS (
  SELECT
    b.project_id,
    b.date,
    b.country_code,
    b.continent,
    b.asn,
    b.isp_name,
    b.ip_hash,
    b.bytes_sent,
    (
      flags.has_known_bot_ua
      OR
      flags.has_probe_path
      OR flags.short_404_probes >= 2
      OR (flags.crawler_utility_requests > 0 AND flags.non_utility_requests = 0)
    ) AS is_bot
  FROM buffer_rows b
  JOIN ip_day_flags flags
    ON flags.project_id = b.project_id
   AND flags.date = b.date
   AND flags.ip_hash = b.ip_hash
)`, dayExpr, dayExpr, dayLiteral, botProbe, short404Probe, crawlerUtility, crawlerUtility))
}

func AggregateSQL(targetDate time.Time) string {
	return strings.TrimSpace(fmt.Sprintf(`
%s,
daily_rows AS (
  SELECT
    project_id,
    date,
    country_code,
    continent,
    asn,
    isp_name,
    COUNT(DISTINCT ip_hash)::INTEGER AS visitors,
    COUNT(*)::INTEGER AS requests,
    ROUND(SUM(bytes_sent)::NUMERIC / 1048576.0, 2) AS bandwidth_mb,
    COUNT(DISTINCT ip_hash) FILTER (WHERE NOT is_bot)::INTEGER AS human_visitors,
    COUNT(*) FILTER (WHERE NOT is_bot)::INTEGER AS human_requests,
    ROUND(COALESCE(SUM(bytes_sent) FILTER (WHERE NOT is_bot), 0)::NUMERIC / 1048576.0, 2) AS human_bandwidth_mb
  FROM classified_rows
  GROUP BY project_id, date, country_code, continent, asn, isp_name
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
  bandwidth_mb,
  human_visitors,
  human_requests,
  human_bandwidth_mb
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
  bandwidth_mb,
  human_visitors,
  human_requests,
  human_bandwidth_mb
FROM daily_rows
ON CONFLICT (project_id, date, country_code, continent, asn, isp_name)
DO UPDATE SET
  visitors = EXCLUDED.visitors,
  requests = EXCLUDED.requests,
  bandwidth_mb = EXCLUDED.bandwidth_mb,
  human_visitors = EXCLUDED.human_visitors,
  human_requests = EXCLUDED.human_requests,
  human_bandwidth_mb = EXCLUDED.human_bandwidth_mb;

DELETE FROM analytics_buffer
WHERE timestamp < timezone('UTC', NOW()) - INTERVAL '48 hours';
`, humanClassificationCTE(targetDate)))
}
