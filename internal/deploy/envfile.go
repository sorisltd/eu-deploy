package deploy

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

var localDeployEnvFiles = []string{
	".env.local",
	".env.deploy.local",
}

func LoadLocalDeployEnvFiles(workDir string) (map[string]string, error) {
	values := map[string]string{}

	for _, name := range localDeployEnvFiles {
		path := filepath.Join(workDir, name)
		parsed, err := parseEnvFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for key, value := range parsed {
			values[key] = value
		}
	}

	return values, nil
}

func parseEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, rawValue, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if !validEnvFileKey(key) {
			continue
		}

		values[key] = normalizeEnvFileValue(rawValue)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return values, nil
}

func normalizeEnvFileValue(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) < 2 {
		return value
	}

	switch {
	case strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`):
		unquoted := value[1 : len(value)-1]
		replacer := strings.NewReplacer(
			`\\`, `\`,
			`\n`, "\n",
			`\r`, "\r",
			`\t`, "\t",
			`\"`, `"`,
		)
		return replacer.Replace(unquoted)
	case strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'"):
		return value[1 : len(value)-1]
	default:
		return value
	}
}

func validEnvFileKey(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		case r == '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
