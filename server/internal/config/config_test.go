package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileRequiresSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	if err := os.WriteFile(path, []byte("LISTEN_ADDR=:9090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFile(path); err == nil {
		t.Fatal("expected missing keys")
	}
}

func TestLoadFileParsesValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	body := "" +
		"# comment\n" +
		"\n" +
		"export LISTEN_ADDR=:9090\n" +
		"VIEW_PASSWORD=\"pw\"\n" +
		"INGEST_API_KEY='key=1'\n" +
		"SESSION_SECRET=secret\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":9090" || cfg.ViewPassword != "pw" || cfg.IngestAPIKey != "key=1" || cfg.SessionSecret != "secret" {
		t.Fatalf("unexpected config %+v", cfg)
	}
}

func TestLoadFileDefaultsListenAddr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	body := "VIEW_PASSWORD=pw\nINGEST_API_KEY=key\nSESSION_SECRET=secret\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Fatalf("listen addr %q", cfg.ListenAddr)
	}
}

func TestFirstExistingPrefersExecutableDirectory(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	wd := filepath.Join(root, "work")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	exeFile := filepath.Join(exeDir, "config.env")
	wdFile := filepath.Join(wd, "config.env")
	if err := os.WriteFile(exeFile, []byte("exe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wdFile, []byte("wd\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := firstExisting(exeDir, wd)
	if err != nil {
		t.Fatal(err)
	}
	if got != exeFile {
		t.Fatalf("got %s", got)
	}

	if err := os.Remove(exeFile); err != nil {
		t.Fatal(err)
	}
	got, err = firstExisting(exeDir, wd)
	if err != nil {
		t.Fatal(err)
	}
	if got != wdFile {
		t.Fatalf("got %s", got)
	}
}
