package engine

import (
	"os"
	"strings"
)

var (
	POD_IP        string
	POD_NAMESPACE string
	POD_NAME      string
)

func init() {
	POD_IP = readEnvTrimmed("POD_IP")
	POD_NAMESPACE = readEnvTrimmed("POD_NAMESPACE")
	POD_NAME = readEnvTrimmed("POD_NAME")
}

func readEnvTrimmed(key string) string {
	out := strings.TrimSpace(os.Getenv(key))
	if out == "" {
		panic("Critical Error: " + key + " is not set. POD_IP, POD_NAMESPACE and POD_NAME must be set.")
	}
	return out
}
