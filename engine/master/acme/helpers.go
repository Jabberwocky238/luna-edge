package acme

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

func certificateArtifactPrefix(prefix, hostname string, revision uint64) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = "certificates"
	}
	return fmt.Sprintf("%s/%s/%d", prefix, hostname, revision)
}

func randomID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

func normalizeString(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
