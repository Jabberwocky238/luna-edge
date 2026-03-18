package utils

import (
	"errors"
	"regexp"
	"strings"
)

type DomainName struct {
	Hostname string
	Wildcard bool
}

func TryDomainName(f string) *DomainName {
	hostname, wildcard, err := normalizeHost(f)
	if err != nil {
		return nil
	}
	return &DomainName{
		Hostname: hostname,
		Wildcard: wildcard,
	}
}

func (f *DomainName) String() string {
	return f.Hostname
}

var hostnameLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeHost(host string) (string, bool, error) {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return sanitizeHostname(host)
}

func sanitizeHostname(host string) (string, bool, error) {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return "", false, errors.New("hostname cannot be empty")
	}

	wildcard := false
	if strings.HasPrefix(host, "*.") {
		wildcard = true
		host = strings.TrimPrefix(host, "*.")
	}
	if host == "" || strings.Contains(host, "..") || strings.ContainsAny(host, `/\ `) {
		return "", false, errors.New("invalid hostname")
	}

	labels := strings.SplitSeq(host, ".")
	for label := range labels {
		if !hostnameLabelPattern.MatchString(label) {
			return "", false, errors.New("invalid hostname label: " + label)
		}
	}

	if wildcard {
		return "*." + host, true, nil
	}
	return host, false, nil
}
