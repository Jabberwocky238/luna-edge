package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
)

func loadBody(inline, filePath string) ([]byte, error) {
	switch {
	case inline != "":
		return []byte(inline), nil
	case filePath == "-":
		return io.ReadAll(os.Stdin)
	case filePath != "":
		return os.ReadFile(filePath)
	default:
		stat, err := os.Stdin.Stat()
		if err != nil {
			return nil, err
		}
		if stat.Mode()&os.ModeCharDevice == 0 {
			return io.ReadAll(os.Stdin)
		}
		return nil, nil
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func lookupSupportedResource(name string) (lnctlkit.ResourceInfo, bool) {
	for _, res := range lnctlkit.SupportedResources() {
		if res.Name == name {
			return res, true
		}
	}
	return lnctlkit.ResourceInfo{}, false
}

func printJSON(v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(body); err != nil {
		return err
	}
	if !bytes.HasSuffix(body, []byte{'\n'}) {
		_, err = os.Stdout.Write([]byte{'\n'})
	}
	return err
}
