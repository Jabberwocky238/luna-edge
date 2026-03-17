package utiltypes

import (
	"regexp"
	"strings"
)

type DomainName string

func StrToDomainName(f string) DomainName {
	return DomainName(normalizeHost(f))
}

var hostnameLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return sanitizeHostname(host)
}

func sanitizeHostname(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}

	wildcard := false
	if strings.HasPrefix(host, "*.") {
		wildcard = true
		host = strings.TrimPrefix(host, "*.")
	}
	if host == "" || strings.Contains(host, "..") || strings.ContainsAny(host, `/\ `) {
		return ""
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !hostnameLabelPattern.MatchString(label) {
			return ""
		}
	}

	if wildcard {
		return "*." + host
	}
	return host
}
