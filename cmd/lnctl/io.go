package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
)

var planNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type planStore struct {
	dir string
}

func newPlanStore(dir string) (*planStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("plan store dir is required")
	}
	return &planStore{dir: dir}, nil
}

func (s *planStore) Save(name string, plan *lnctlkit.Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is required")
	}
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}
	body, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write plan file: %w", err)
	}
	return nil
}

func (s *planStore) Load(name string) (*lnctlkit.Plan, error) {
	path, err := s.path(name)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan file: %w", err)
	}
	var plan lnctlkit.Plan
	if err := json.Unmarshal(body, &plan); err != nil {
		return nil, fmt.Errorf("decode plan file: %w", err)
	}
	return &plan, nil
}

func (s *planStore) Delete(name string) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete plan file: %w", err)
	}
	return nil
}

func (s *planStore) Exists(name string) bool {
	path, err := s.path(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func (s *planStore) path(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("plan name is required")
	}
	if !planNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid plan name %q", name)
	}
	return filepath.Join(s.dir, name+".json"), nil
}

func loadBody(stdin *os.File, inline, filePath string) ([]byte, error) {
	inline = strings.TrimSpace(inline)
	filePath = strings.TrimSpace(filePath)

	switch {
	case inline != "" && filePath != "":
		return nil, fmt.Errorf("use only one of -d or -f")
	case inline != "":
		return []byte(inline), nil
	case filePath == "-":
		return io.ReadAll(stdin)
	case filePath != "":
		return os.ReadFile(filePath)
	default:
		if stdin == nil {
			return nil, nil
		}
		stat, err := stdin.Stat()
		if err != nil {
			return nil, err
		}
		if stat.Mode()&os.ModeCharDevice != 0 {
			return nil, nil
		}
		return io.ReadAll(stdin)
	}
}
