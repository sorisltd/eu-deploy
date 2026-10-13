package deploy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sorisltd/eu-deploy/internal/migrations"
)

// MigrationResult is what `eu migrate` reports (and prints with --json).
type MigrationResult struct {
	Mode    string `json:"mode"`
	Applied int    `json:"applied"`
	Pending int    `json:"pending"`
	Ran     int    `json:"ran"`
	Backup  string `json:"backup,omitempty"`
}

func migrationRunnerOptions(m MigrationOptions) migrations.RunnerOptions {
	return migrations.RunnerOptions{
		Container:   m.Container,
		Database:    m.Database,
		User:        m.User,
		Backup:      m.Backup,
		BackupDir:   m.BackupDir,
		KeepBackups: m.KeepBackups,
	}
}

func validateMigrationOptions(m MigrationOptions) error {
	switch {
	case strings.TrimSpace(m.Dir) == "":
		return fmt.Errorf("database.migrations.dir is required")
	case strings.TrimSpace(m.Container) == "":
		return fmt.Errorf("database.migrations.container is required unless database.mode is shared")
	case strings.TrimSpace(m.Database) == "":
		return fmt.Errorf("database.migrations.database is required unless database.mode is shared")
	case strings.TrimSpace(m.User) == "":
		return fmt.Errorf("database.migrations.user is empty")
	case m.Backup && strings.TrimSpace(m.BackupDir) == "":
		return fmt.Errorf("migration backup folder is empty")
	}
	return nil
}

// writeMigrationBundle plans the migrations locally and writes the archive and
// runner into dir. Bad file names fail the deploy before anything is uploaded.
func writeMigrationBundle(m MigrationOptions, dir string) error {
	if err := validateMigrationOptions(m); err != nil {
		return err
	}
	files, err := migrations.Plan(m.Dir)
	if err != nil {
		return err
	}
	if err := migrations.PackageArchive(m.Dir, files, filepath.Join(dir, migrations.ArchiveName)); err != nil {
		return err
	}
	runner := migrations.RenderRunner(files, migrationRunnerOptions(m))
	return os.WriteFile(filepath.Join(dir, migrations.RunnerName), []byte(runner), 0o755)
}

func migrationScratchPath(opts RemoteOptions) string {
	return filepath.ToSlash(filepath.Join(opts.RemoteAppPath, "migrate-run"))
}

// RunRemoteMigrations runs `status`, `baseline` or `apply` on the remote host
// without deploying. Progress goes to stderr; the result is parsed from the
// runner's result line.
func RunRemoteMigrations(opts RemoteOptions, mode, through string) (MigrationResult, error) {
	if err := validateRemoteOptions(opts); err != nil {
		return MigrationResult{}, err
	}
	if opts.Migrations == nil {
		return MigrationResult{}, fmt.Errorf("database.migrations is not configured in eudeploy.yaml")
	}
	switch mode {
	case "status", "apply", "baseline":
	default:
		return MigrationResult{}, fmt.Errorf("unknown migrate mode %q (status|apply|baseline)", mode)
	}
	if through != "" && mode != "baseline" {
		return MigrationResult{}, fmt.Errorf("--through only applies to baseline")
	}

	localDir, err := os.MkdirTemp("", "eudeploy-migrate-*")
	if err != nil {
		return MigrationResult{}, err
	}
	defer os.RemoveAll(localDir)
	if err := writeMigrationBundle(*opts.Migrations, localDir); err != nil {
		return MigrationResult{}, err
	}

	remoteDir := migrationScratchPath(opts)
	if err := runRemoteScript(opts, fmt.Sprintf("set -euo pipefail\nrm -rf %s\nmkdir -p %s", shellQuote(remoteDir), shellQuote(remoteDir))); err != nil {
		return MigrationResult{}, err
	}
	args := buildSCPArgs(opts)
	args = append(args,
		filepath.Join(localDir, migrations.ArchiveName),
		filepath.Join(localDir, migrations.RunnerName),
		fmt.Sprintf("%s@%s:%s/", opts.RemoteUser, opts.RemoteHost, remoteDir))
	upload := exec.Command("scp", args...)
	upload.Stdout = os.Stderr
	upload.Stderr = os.Stderr
	if err := upload.Run(); err != nil {
		return MigrationResult{}, fmt.Errorf("upload migrations: %w", err)
	}

	command := fmt.Sprintf("set -euo pipefail\ncd %s\nEU_MIGRATE_WHO='eu migrate' bash %s %s %s",
		shellQuote(remoteDir), migrations.RunnerName, shellQuote(mode), shellQuote(through))
	var stdout bytes.Buffer
	sshArgs := buildSSHArgs(opts, false)
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", opts.RemoteUser, opts.RemoteHost), "bash -se")
	run := exec.Command("ssh", sshArgs...)
	run.Stdin = strings.NewReader(command)
	run.Stdout = &stdout
	run.Stderr = os.Stderr
	runErr := run.Run()
	if runErr != nil {
		return MigrationResult{}, fmt.Errorf("migrate %s failed on the server (see output above)", mode)
	}
	return parseMigrationResult(&stdout)
}

func parseMigrationResult(r io.Reader) (MigrationResult, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 6 || fields[0] != migrations.ResultPrefix {
			continue
		}
		res := MigrationResult{Mode: fields[1]}
		var err error
		if res.Applied, err = strconv.Atoi(fields[2]); err != nil {
			return res, fmt.Errorf("bad migrate result line: %q", scanner.Text())
		}
		if res.Pending, err = strconv.Atoi(fields[3]); err != nil {
			return res, fmt.Errorf("bad migrate result line: %q", scanner.Text())
		}
		if res.Ran, err = strconv.Atoi(fields[4]); err != nil {
			return res, fmt.Errorf("bad migrate result line: %q", scanner.Text())
		}
		if fields[5] != "-" {
			res.Backup = fields[5]
		}
		return res, nil
	}
	return MigrationResult{}, fmt.Errorf("migrate finished without a result line")
}
