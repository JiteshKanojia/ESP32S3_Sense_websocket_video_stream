package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fileName = "config.env"

// Config is loaded from config.env. Secrets are required so a public
// tunnel cannot come up with an empty password or API key.
type Config struct {
	ListenAddr    string
	ViewPassword  string
	IngestAPIKey  string
	SessionSecret string
}

// Load reads config.env beside the executable. If that file is absent, it
// reads config.env in the working directory so `go run` still finds it.
func Load() (Config, error) {
	path, err := findConfigFile()
	if err != nil {
		return Config{}, err
	}
	return loadFile(path)
}

func findConfigFile() (string, error) {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dirs = append(dirs, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	return firstExisting(dirs...)
}

func firstExisting(dirs ...string) (string, error) {
	var tried []string
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		path := filepath.Join(dir, fileName)
		tried = append(tried, path)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
	}
	if len(tried) == 0 {
		return "", fmt.Errorf("%s not found", fileName)
	}
	return "", fmt.Errorf("%s not found (looked in %s)", fileName, strings.Join(tried, ", "))
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	cfg, err := fromMap(parseEnv(string(data)))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func fromMap(vals map[string]string) (Config, error) {
	cfg := Config{
		ListenAddr:    vals["LISTEN_ADDR"],
		ViewPassword:  vals["VIEW_PASSWORD"],
		IngestAPIKey:  vals["INGEST_API_KEY"],
		SessionSecret: vals["SESSION_SECRET"],
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	var missing []string
	if cfg.ViewPassword == "" {
		missing = append(missing, "VIEW_PASSWORD")
	}
	if cfg.IngestAPIKey == "" {
		missing = append(missing, "INGEST_API_KEY")
	}
	if cfg.SessionSecret == "" {
		missing = append(missing, "SESSION_SECRET")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func parseEnv(text string) map[string]string {
	vals := make(map[string]string)
	text = strings.TrimPrefix(text, "\uFEFF")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			quote := val[0]
			if (quote == '"' || quote == '\'') && val[len(val)-1] == quote {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" {
			vals[key] = val
		}
	}
	return vals
}
