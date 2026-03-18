package utils

import "log"

const (
	certColorPrefix = "\033[1;33m[CERT]\033[0m "
)

func CertLogf(format string, args ...any) {
	log.Printf(certColorPrefix+format, args...)
}
