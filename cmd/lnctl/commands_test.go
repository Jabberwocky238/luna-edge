package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
)

func TestPlanStoreSaveLoadDelete(t *testing.T) {
	store, err := newPlanStore(t.TempDir())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}

	plan := &lnctlkit.Plan{Hostname: "app.example.com"}
	if err := store.Save("app", plan); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load("app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Hostname != "app.example.com" {
		t.Fatalf("unexpected plan: %+v", got)
	}

	if err := store.Delete("app"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.Exists("app") {
		t.Fatal("expected plan to be deleted")
	}
}

func TestLoadBodyFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(`{"Hostname":"x"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer stdinRead.Close()
	defer stdinWrite.Close()

	body, err := loadBody(stdinRead, "", path)
	if err != nil {
		t.Fatalf("loadBody: %v", err)
	}
	if string(body) != `{"Hostname":"x"}` {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestExecuteBuildCreateAndEdit(t *testing.T) {
	dir := t.TempDir()
	store, err := newPlanStore(dir)
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer stdinRead.Close()
	defer stdinWrite.Close()

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer stdout.Close()

	app := &app{
		store:  store,
		stdin:  stdinRead,
		stdout: stdout,
		stderr: stdout,
	}

	if err := executeBuildCreate(app, "demo", []string{"-d", `{"Hostname":"demo.example.com"}`}); err != nil {
		t.Fatalf("executeBuildCreate: %v", err)
	}

	if err := executeBuildEdit(app, "demo", []string{"-d", `{"Hostname":"edited.example.com"}`}); err != nil {
		t.Fatalf("executeBuildEdit: %v", err)
	}

	got, err := store.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Hostname != "edited.example.com" {
		t.Fatalf("unexpected stored plan: %+v", got)
	}
}

func TestPrintJSON(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer file.Close()

	if err := printJSON(file, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("printJSON: %v", err)
	}
	body, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(body, []byte(`"a": "b"`)) {
		t.Fatalf("unexpected output: %s", string(body))
	}
}
