package main

import (
	"flag"
	"fmt"
)

func executeResourceCommand(app *app, resource, command string, args []string) error {
	switch command {
	case "ls", "list":
		if len(args) != 0 {
			return fmt.Errorf("%s ls does not accept arguments", resource)
		}
		return executeList(app, resource)
	case "get":
		if len(args) != 1 {
			return fmt.Errorf("usage: lnctl %s get <id>", resource)
		}
		return executeGet(app, resource, args[0])
	case "put":
		return executePut(app, resource, args)
	case "rm", "del", "delete":
		if len(args) != 1 {
			return fmt.Errorf("usage: lnctl %s rm <id>", resource)
		}
		return executeDelete(app, resource, args[0])
	default:
		return fmt.Errorf("unsupported command %q for resource %s", command, resource)
	}
}

func executeList(app *app, resource string) error {
	rc, err := app.client.ManageResource(resource)
	if err != nil {
		return err
	}
	v, err := rc.ListAny()
	return printResult(v, err)
}

func executeGet(app *app, resource, id string) error {
	rc, err := app.client.ManageResource(resource)
	if err != nil {
		return err
	}
	v, err := rc.GetAny(id)
	return printResult(v, err)
}

func executePut(app *app, resource string, args []string) error {
	fs := flag.NewFlagSet(resource+" put", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var inline string
	var filePath string
	fs.StringVar(&inline, "d", "", "inline json")
	fs.StringVar(&filePath, "f", "", "json file path, use - for stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) > 1 {
		return fmt.Errorf("usage: lnctl %s put [-d JSON|-f FILE]", resource)
	}

	body, err := loadBody(inline, filePath)
	if err != nil {
		return fmt.Errorf("load body: %w", err)
	}
	if len(body) == 0 {
		return fmt.Errorf("request body is required, use -d, -f, or stdin")
	}

	rc, err := app.client.ManageResource(resource)
	if err != nil {
		return err
	}
	v, err := rc.PutJSON(body)
	return printResult(v, err)
}

func executeDelete(app *app, resource, id string) error {
	rc, err := app.client.ManageResource(resource)
	if err != nil {
		return err
	}
	return rc.Delete(id)
}

func printResult(v any, err error) error {
	if err != nil {
		return err
	}
	return printJSON(v)
}
