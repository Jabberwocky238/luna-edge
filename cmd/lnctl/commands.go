package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	lnctlkit "github.com/jabberwocky238/luna-edge/lnctl"
)

func executeCommand(app *app, command string, args []string) error {
	switch command {
	case "query":
		return executeQueryCommand(app, args)
	case "apply":
		return executeApplyCommand(app, args)
	case "build":
		return executeBuildCommand(app, args)
	case "help":
		printUsage(app.stdout)
		return nil
	default:
		return fmt.Errorf("unsupported command %q", command)
	}
}

func executeQueryCommand(app *app, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lnctl query <domain|dns> [flags]")
	}
	switch args[0] {
	case "domain":
		return executeQueryDomain(app, args[1:])
	case "dns":
		return executeQueryDNS(app, args[1:])
	default:
		return fmt.Errorf("unsupported query target %q", args[0])
	}
}

func executeQueryDomain(app *app, args []string) error {
	fs := flag.NewFlagSet("query domain", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var hostname string
	fs.StringVar(&hostname, "hostname", "", "domain hostname")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: lnctl query domain --hostname HOSTNAME")
	}
	value, err := app.client.QueryDomainEntryProjection(hostname)
	if err != nil {
		return err
	}
	return printJSON(app.stdout, value)
}

func executeQueryDNS(app *app, args []string) error {
	fs := flag.NewFlagSet("query dns", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var fqdn string
	var recordType string
	fs.StringVar(&fqdn, "fqdn", "", "dns fqdn")
	fs.StringVar(&recordType, "record-type", "", "dns record type")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: lnctl query dns --fqdn FQDN --record-type TYPE")
	}
	value, err := app.client.QueryDNSRecords(fqdn, recordType)
	if err != nil {
		return err
	}
	return printJSON(app.stdout, value)
}

func executeApplyCommand(app *app, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: lnctl apply <plan-name>")
	}
	plan, err := app.store.Load(args[0])
	if err != nil {
		return err
	}
	applied, err := app.client.ApplyPlan(plan)
	if err != nil {
		return err
	}
	return printJSON(app.stdout, applied)
}

func executeBuildCommand(app *app, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: lnctl build <create|show|edit|delete> <plan-name> [flags]")
	}
	switch args[0] {
	case "create":
		return executeBuildCreate(app, args[1], args[2:])
	case "show":
		return executeBuildShow(app, args[1], args[2:])
	case "edit":
		return executeBuildEdit(app, args[1], args[2:])
	case "delete":
		return executeBuildDelete(app, args[1], args[2:])
	default:
		return fmt.Errorf("unsupported build action %q", args[0])
	}
}

func executeBuildCreate(app *app, name string, args []string) error {
	fs := flag.NewFlagSet("build create", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var inline string
	var filePath string
	fs.StringVar(&inline, "d", "", "inline plan json")
	fs.StringVar(&filePath, "f", "", "plan json file, use - for stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: lnctl build create <plan-name> [-f FILE|-d JSON]")
	}
	if app.store.Exists(name) {
		return fmt.Errorf("plan %q already exists", name)
	}
	plan, err := decodePlanInput(app.stdin, inline, filePath)
	if err != nil {
		return err
	}
	if plan == nil {
		plan = &lnctlkit.Plan{}
	}
	if err := app.store.Save(name, plan); err != nil {
		return err
	}
	return printJSON(app.stdout, plan)
}

func executeBuildShow(app *app, name string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: lnctl build show <plan-name>")
	}
	plan, err := app.store.Load(name)
	if err != nil {
		return err
	}
	return printJSON(app.stdout, plan)
}

func executeBuildEdit(app *app, name string, args []string) error {
	fs := flag.NewFlagSet("build edit", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var inline string
	var filePath string
	fs.StringVar(&inline, "d", "", "inline plan json")
	fs.StringVar(&filePath, "f", "", "plan json file, use - for stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: lnctl build edit <plan-name> [-f FILE|-d JSON]")
	}
	plan, err := decodePlanInput(app.stdin, inline, filePath)
	if err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("edited plan body is required, use -d, -f, or stdin")
	}
	if !app.store.Exists(name) {
		return fmt.Errorf("plan %q does not exist", name)
	}
	if err := app.store.Save(name, plan); err != nil {
		return err
	}
	return printJSON(app.stdout, plan)
}

func executeBuildDelete(app *app, name string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: lnctl build delete <plan-name>")
	}
	return app.store.Delete(name)
}

func decodePlanInput(stdin *os.File, inline, filePath string) (*lnctlkit.Plan, error) {
	body, err := loadBody(stdin, inline, filePath)
	if err != nil {
		return nil, fmt.Errorf("load plan body: %w", err)
	}
	if len(body) == 0 {
		return nil, nil
	}
	var plan lnctlkit.Plan
	if err := json.Unmarshal(body, &plan); err != nil {
		return nil, fmt.Errorf("decode plan body: %w", err)
	}
	return &plan, nil
}
