package acme

import "log"

const (
	certColorPrefix = "\033[1;36m[CERT]\033[0m "
)

func certLogf(format string, args ...any) {
	log.Printf(certColorPrefix+format, args...)
}
