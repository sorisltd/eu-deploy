package analytics

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/oschwald/geoip2-golang"
)

type Project struct {
	ID          int64
	Name        string
	Hostname    string
	LogPath     string
	Metadata    string
	LogFileName string
}

type ProcessorState struct {
	LogPath    string
	Inode      uint64
	ByteOffset int64
}

type projectMetadata struct {
	ProjectName string   `json:"projectName"`
	Hostnames   []string `json:"hostnames"`
	LogPath     string   `json:"logPath"`
}

type caddyLogEntry struct {
	TS      json.RawMessage `json:"ts"`
	Status  int             `json:"status"`
	Size    json.RawMessage `json:"size"`
	Request struct {
		RemoteIP string              `json:"remote_ip"`
		ClientIP string              `json:"client_ip"`
		URI      string              `json:"uri"`
		Headers  map[string][]string `json:"headers"`
	} `json:"request"`
}

type logRecord struct {
	ProjectID   int64
	Timestamp   time.Time
	IPHash      string
	CountryCode string
	Continent   string
	ASN         sql.NullInt64
	ISPName     sql.NullString
	Path        sql.NullString
	StatusCode  sql.NullInt64
	IsKnownBot  bool
	BytesSent   int64
}

type geoLookup struct {
	countryCode string
	continent   string
	asn         sql.NullInt64
	ispName     sql.NullString
}

func EnsureSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, SchemaSQL)
	return err
}

func Process(ctx context.Context, cfg Config) error {
	db, err := OpenDatabase(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := EnsureSchema(ctx, db); err != nil {
		return err
	}
	if err := EnsureSecretFile(cfg.SecretFile); err != nil {
		return err
	}
	secret, err := ReadSecret(cfg.SecretFile)
	if err != nil {
		return err
	}

	projects, err := syncProjects(ctx, db, cfg)
	if err != nil {
		return err
	}

	cityReader, _ := openGeoReaderIfExists(cfg.CityDBPath)
	if cityReader != nil {
		defer cityReader.Close()
	}
	asnReader, _ := openGeoReaderIfExists(cfg.ASNDBPath)
	if asnReader != nil {
		defer asnReader.Close()
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	insertStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO analytics_buffer (
		  project_id,
		  timestamp,
		  ip_hash,
		  country_code,
		  continent,
		  asn,
		  isp_name,
		  path,
		  status_code,
		  is_known_bot,
		  bytes_sent
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()

	for _, project := range projects {
		state, stateErr := fetchState(ctx, tx, project.LogPath)
		if stateErr != nil {
			return stateErr
		}

		nextState, records, readErr := readProjectLog(project, state, secret, cityReader, asnReader)
		if readErr != nil {
			return readErr
		}

		for _, record := range records {
			if _, execErr := insertStmt.ExecContext(
				ctx,
				record.ProjectID,
				record.Timestamp.UTC(),
				record.IPHash,
				record.CountryCode,
				record.Continent,
				record.ASN,
				record.ISPName,
				record.Path,
				record.StatusCode,
				record.IsKnownBot,
				record.BytesSent,
			); execErr != nil {
				return execErr
			}
		}

		if err := upsertState(ctx, tx, nextState); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func syncProjects(ctx context.Context, db *sql.DB, cfg Config) ([]Project, error) {
	entries, err := os.ReadDir(cfg.AppsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		project, projectErr := projectFromDir(cfg, entry.Name())
		if projectErr != nil {
			return nil, projectErr
		}

		if _, execErr := db.ExecContext(
			ctx,
			`
				INSERT INTO analytics_projects (project_name, hostname, log_path, updated_at)
				VALUES ($1, $2, $3, NOW())
				ON CONFLICT (project_name) DO UPDATE
				SET hostname = EXCLUDED.hostname,
				    log_path = EXCLUDED.log_path,
				    updated_at = NOW()
			`,
			project.Name,
			nullIfEmpty(project.Hostname),
			project.LogPath,
		); execErr != nil {
			return nil, execErr
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, project_name, COALESCE(hostname, ''), log_path
		FROM analytics_projects
		ORDER BY project_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var project Project
		if scanErr := rows.Scan(&project.ID, &project.Name, &project.Hostname, &project.LogPath); scanErr != nil {
			return nil, scanErr
		}
		projects = append(projects, project)
	}

	return projects, rows.Err()
}

func projectFromDir(cfg Config, dirName string) (Project, error) {
	name := strings.TrimSpace(dirName)
	project := Project{
		Name:        name,
		LogPath:     filepath.Join(cfg.LogsDir, fmt.Sprintf("%s.access.log", name)),
		LogFileName: fmt.Sprintf("%s.access.log", name),
	}

	metadataPath := filepath.Join(cfg.AppsDir, dirName, "analytics-project.json")
	contents, err := os.ReadFile(metadataPath)
	if err == nil {
		var metadata projectMetadata
		if unmarshalErr := json.Unmarshal(contents, &metadata); unmarshalErr != nil {
			return project, unmarshalErr
		}
		if strings.TrimSpace(metadata.ProjectName) != "" {
			project.Name = strings.TrimSpace(metadata.ProjectName)
		}
		if len(metadata.Hostnames) > 0 {
			project.Hostname = strings.TrimSpace(metadata.Hostnames[0])
		}
		if strings.TrimSpace(metadata.LogPath) != "" {
			project.LogPath = strings.TrimSpace(metadata.LogPath)
		}
		return project, nil
	}
	if !os.IsNotExist(err) {
		return project, err
	}

	return project, nil
}

func fetchState(ctx context.Context, tx *sql.Tx, logPath string) (ProcessorState, error) {
	state := ProcessorState{LogPath: logPath}

	row := tx.QueryRowContext(ctx, `
		SELECT COALESCE(inode, 0), COALESCE(byte_offset, 0)
		FROM analytics_processor_state
		WHERE log_path = $1
	`, logPath)

	var inode int64
	if err := row.Scan(&inode, &state.ByteOffset); err != nil {
		if err == sql.ErrNoRows {
			return state, nil
		}
		return state, err
	}

	state.Inode = uint64(inode)
	return state, nil
}

func upsertState(ctx context.Context, tx *sql.Tx, state ProcessorState) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO analytics_processor_state (log_path, inode, byte_offset, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (log_path) DO UPDATE
		SET inode = EXCLUDED.inode,
		    byte_offset = EXCLUDED.byte_offset,
		    updated_at = NOW()
	`, state.LogPath, int64(state.Inode), state.ByteOffset)

	return err
}

func readProjectLog(
	project Project,
	state ProcessorState,
	secret string,
	cityReader *geoip2.Reader,
	asnReader *geoip2.Reader,
) (ProcessorState, []logRecord, error) {
	file, err := os.Open(project.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ProcessorState{LogPath: project.LogPath}, nil, nil
		}
		return state, nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return state, nil, err
	}

	inode := inodeFromInfo(info)
	offset := state.ByteOffset
	if state.Inode != inode || offset > info.Size() || offset < 0 {
		offset = 0
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return state, nil, err
	}

	reader := bufio.NewReader(file)
	records := make([]logRecord, 0, 64)
	nextOffset := offset

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			nextOffset += int64(len(line))
			record, parseErr := recordFromLine(project, secret, cityReader, asnReader, line)
			if parseErr == nil {
				records = append(records, record)
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return state, nil, readErr
		}
	}

	return ProcessorState{
		LogPath:    project.LogPath,
		Inode:      inode,
		ByteOffset: nextOffset,
	}, records, nil
}

func recordFromLine(
	project Project,
	secret string,
	cityReader *geoip2.Reader,
	asnReader *geoip2.Reader,
	line []byte,
) (logRecord, error) {
	var entry caddyLogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return logRecord{}, err
	}

	ipText := strings.TrimSpace(entry.Request.ClientIP)
	if ipText == "" {
		ipText = strings.TrimSpace(entry.Request.RemoteIP)
	}
	ip := net.ParseIP(ipText)
	if ip == nil {
		return logRecord{}, fmt.Errorf("missing or invalid IP")
	}

	timestamp, err := parseTimestamp(entry.TS)
	if err != nil {
		return logRecord{}, err
	}

	path := cleanPath(entry.Request.URI)
	userAgent := firstHeaderValue(entry.Request.Headers, "User-Agent")
	lookup := lookupGeo(ip, cityReader, asnReader)
	bytesSent := parseInt64(entry.Size)

	return logRecord{
		ProjectID:   project.ID,
		Timestamp:   timestamp,
		IPHash:      hashIP(secret, timestamp, ip.String()),
		CountryCode: lookup.countryCode,
		Continent:   lookup.continent,
		ASN:         lookup.asn,
		ISPName:     lookup.ispName,
		Path:        nullIfEmpty(path),
		StatusCode:  nullableInt64(int64(entry.Status)),
		IsKnownBot:  isKnownBotUserAgent(userAgent),
		BytesSent:   bytesSent,
	}, nil
}

func lookupGeo(ip net.IP, cityReader *geoip2.Reader, asnReader *geoip2.Reader) geoLookup {
	lookup := geoLookup{
		countryCode: "ZZ",
		continent:   "UN",
	}

	if cityReader != nil {
		if city, err := cityReader.City(ip); err == nil && city != nil {
			if code := strings.ToUpper(strings.TrimSpace(city.Country.IsoCode)); code != "" {
				lookup.countryCode = code
			}
			if continent := strings.ToUpper(strings.TrimSpace(city.Continent.Code)); continent != "" {
				lookup.continent = continent
			}
		}
	}

	if asnReader != nil {
		if asn, err := asnReader.ASN(ip); err == nil && asn != nil {
			if asn.AutonomousSystemNumber > 0 {
				lookup.asn = nullableInt64(int64(asn.AutonomousSystemNumber))
			}
			if name := strings.TrimSpace(asn.AutonomousSystemOrganization); name != "" {
				lookup.ispName = nullIfEmpty(name)
			}
		}
	}

	if !lookup.ispName.Valid {
		lookup.ispName = nullIfEmpty("Unknown network")
	}

	return lookup
}

func openGeoReaderIfExists(path string) (*geoip2.Reader, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return geoip2.Open(path)
}

func parseTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 {
		return time.Now().UTC(), nil
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		seconds := int64(asFloat)
		nanos := int64((asFloat - float64(seconds)) * float64(time.Second))
		return time.Unix(seconds, nanos).UTC(), nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if asString == "" {
			return time.Now().UTC(), nil
		}
		if parsed, err := time.Parse(time.RFC3339Nano, asString); err == nil {
			return parsed.UTC(), nil
		}
	}

	return time.Now().UTC(), nil
}

func parseInt64(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}

	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		return asInt
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		return int64(asFloat)
	}

	return 0
}

func firstHeaderValue(headers map[string][]string, key string) string {
	for name, values := range headers {
		if !strings.EqualFold(name, key) || len(values) == 0 {
			continue
		}
		return strings.TrimSpace(values[0])
	}

	return ""
}

func isKnownBotUserAgent(userAgent string) bool {
	value := strings.ToLower(strings.TrimSpace(userAgent))
	if value == "" {
		return false
	}

	knownBotTokens := []string{
		"googlebot",
		"googleother",
		"adsbot",
		"apis-google",
		"mediapartners-google",
		"feedfetcher-google",
		"bingbot",
		"adidxbot",
		"duckduckbot",
		"baiduspider",
		"bytespider",
		"yandexbot",
		"yandexmobilebot",
		"petalbot",
		"applebot",
		"amazonbot",
		"facebookexternalhit",
		"meta-externalagent",
		"meta-externalfetcher",
		"linkedinbot",
		"slackbot",
		"discordbot",
		"twitterbot",
		"redditbot",
		"telegrambot",
		"skypeuripreview",
		"semrushbot",
		"ahrefsbot",
		"mj12bot",
		"dotbot",
		"rogerbot",
		"ccbot",
		"claudebot",
		"gptbot",
		"oai-searchbot",
		"perplexitybot",
		"uptimerobot",
		"pingdom",
		"statuscake",
		"better uptime",
		"datadog/synthetics",
	}

	for _, token := range knownBotTokens {
		if strings.Contains(value, token) {
			return true
		}
	}

	automationTokens := []string{
		"curl/",
		"wget/",
		"python-requests",
		"python-httpx",
		"aiohttp",
		"go-http-client",
		"headlesschrome",
		"phantomjs",
		"selenium",
		"playwright",
		"puppeteer",
		"node-fetch",
		"postmanruntime",
		"insomnia",
		"scrapy",
		"libwww-perl",
		"apache-httpclient",
		"java/",
		"okhttp",
		"chrome-lighthouse",
	}

	for _, token := range automationTokens {
		if strings.Contains(value, token) {
			return true
		}
	}

	return strings.Contains(value, "bot") ||
		strings.Contains(value, "crawler") ||
		strings.Contains(value, "spider")
}

func hashIP(secret string, timestamp time.Time, ip string) string {
	dateSalt := timestamp.UTC().Format("2006-01-02")
	sum := sha256.Sum256([]byte(secret + "|" + dateSalt + "|" + ip))
	return hex.EncodeToString(sum[:])
}

func cleanPath(uri string) string {
	value := strings.TrimSpace(uri)
	if value == "" {
		return ""
	}

	if before, _, found := strings.Cut(value, "?"); found {
		return before
	}

	return value
}

func inodeFromInfo(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}

func nullIfEmpty(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullableInt64(value int64) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}
