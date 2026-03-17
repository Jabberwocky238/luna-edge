package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
)

type app struct {
	client *lnctlkit.Client
}

func Main(args []string) int {
	cfg := parseGlobalConfig(args)
	if cfg.err != nil {
		fatalf("%v", cfg.err)
		return 1
	}
	if cfg.help || cfg.resource == "" {
		printUsage()
		return 0
	}

	if cfg.command == "" || cfg.command == "help" {
		printResourceUsage(cfg.resource)
		return 0
	}

	app := &app{client: lnctlkit.NewClient(cfg.baseURL)}
	if err := executeResourceCommand(app, cfg.resource, cfg.command, cfg.commandArgs); err != nil {
		fatalf("%v", err)
		return 1
	}
	return 0
}

type globalConfig struct {
	baseURL     string
	resource    string
	command     string
	commandArgs []string
	help        bool
	err         error
}

func parseGlobalConfig(args []string) globalConfig {
	fs := flag.NewFlagSet("lnctl", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	cfg := globalConfig{}
	fs.StringVar(&cfg.baseURL, "master", envOr("LUNA_MASTER_MANAGE_URL", "http://luna-master.luna-edge.svc.cluster.local:8080"), "master manage base url")
	fs.BoolVar(&cfg.help, "h", false, "show help")
	fs.BoolVar(&cfg.help, "help", false, "show help")
	if err := fs.Parse(args); err != nil {
		cfg.err = err
		return cfg
	}

	rest := fs.Args()
	if len(rest) > 0 {
		cfg.resource = rest[0]
	}
	if len(rest) > 1 {
		cfg.command = rest[1]
	}
	if len(rest) > 2 {
		cfg.commandArgs = rest[2:]
	}
	return cfg
}

func printUsage() {
	fmt.Fprintf(os.Stdout, "Usage: lnctl [--master URL] <resource> <command> [args]\n\n")
	fmt.Fprintf(os.Stdout, "Commands:\n")
	fmt.Fprintf(os.Stdout, "  ls                list resource objects\n")
	fmt.Fprintf(os.Stdout, "  get <id>          fetch one object\n")
	fmt.Fprintf(os.Stdout, "  put [-d JSON|-f FILE] [id] upsert one object\n")
	fmt.Fprintf(os.Stdout, "  rm <id>           delete one object\n")
	fmt.Fprintf(os.Stdout, "  help              show resource help\n\n")
	fmt.Fprintf(os.Stdout, "Resources:\n")
	for _, res := range lnctlkit.SupportedResources() {
		fmt.Fprintf(os.Stdout, "  %-22s %s\n", res.Name, res.Summary)
	}
}

func printResourceUsage(resource string) {
	fmt.Fprintf(os.Stdout, "Usage: lnctl [--master URL] %s <ls|get|put|rm> [args]\n\n", resource)
	if res, ok := lookupSupportedResource(resource); ok {
		fmt.Fprintf(os.Stdout, "%s\n", res.Summary)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format, args...)
	if !strings.HasSuffix(format, "\n") {
		_, _ = fmt.Fprint(os.Stderr, "\n")
	}
}
