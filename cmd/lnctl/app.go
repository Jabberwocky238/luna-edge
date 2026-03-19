package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
)

type app struct {
	client *lnctlkit.Client
	store  *planStore
	stdin  *os.File
	stdout *os.File
	stderr *os.File
}

type globalConfig struct {
	baseURL     string
	storeDir    string
	command     string
	commandArgs []string
	help        bool
	err         error
}

func Main(args []string) int {
	cfg := parseGlobalConfig(args)
	if cfg.err != nil {
		fatalf(os.Stderr, "%v", cfg.err)
		return 1
	}
	if cfg.help || cfg.command == "" {
		printUsage(os.Stdout)
		return 0
	}

	store, err := newPlanStore(cfg.storeDir)
	if err != nil {
		fatalf(os.Stderr, "%v", err)
		return 1
	}

	app := &app{
		client: lnctlkit.NewClient(cfg.baseURL),
		store:  store,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}

	if err := executeCommand(app, cfg.command, cfg.commandArgs); err != nil {
		fatalf(app.stderr, "%v", err)
		return 1
	}
	return 0
}

func parseGlobalConfig(args []string) globalConfig {
	fs := flag.NewFlagSet("lnctl", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	home, _ := os.UserHomeDir()
	cfg := globalConfig{}
	fs.StringVar(&cfg.baseURL, "master", envOr("LUNA_MASTER_MANAGE_URL", "http://luna-master.luna-edge.svc.cluster.local:8080"), "master manage base url")
	fs.StringVar(&cfg.storeDir, "store", filepath.Join(home, ".lnctl"), "local plan store dir")
	fs.BoolVar(&cfg.help, "h", false, "show help")
	fs.BoolVar(&cfg.help, "help", false, "show help")
	if err := fs.Parse(args); err != nil {
		cfg.err = err
		return cfg
	}

	rest := fs.Args()
	if len(rest) > 0 {
		cfg.command = rest[0]
	}
	if len(rest) > 1 {
		cfg.commandArgs = rest[1:]
	}
	return cfg
}

func printUsage(w *os.File) {
	_, _ = fmt.Fprintln(w, "Usage: lnctl [--master URL] [--store DIR] <query|apply|build> ...")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  query domain --hostname HOSTNAME")
	_, _ = fmt.Fprintln(w, "  query dns --fqdn FQDN --record-type TYPE")
	_, _ = fmt.Fprintln(w, "  apply <plan-name>")
	_, _ = fmt.Fprintln(w, "  build create <plan-name> [-f FILE|-d JSON]")
	_, _ = fmt.Fprintln(w, "  build show <plan-name>")
	_, _ = fmt.Fprintln(w, "  build edit <plan-name> [-f FILE|-d JSON]")
	_, _ = fmt.Fprintln(w, "  build delete <plan-name>")
}

func printJSON(w *os.File, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", body)
	return err
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func fatalf(w *os.File, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
	if !strings.HasSuffix(format, "\n") {
		_, _ = fmt.Fprint(w, "\n")
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
