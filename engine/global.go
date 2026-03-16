package engine

import (
	"os"
	"strings"
)

var (
	POD_IP        string
	POD_NAMESPACE string
)

func init() {
	POD_IP = readEnvTrimmed("POD_IP")
	POD_NAMESPACE = readEnvTrimmed("POD_NAMESPACE")
}

func readEnvTrimmed(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
