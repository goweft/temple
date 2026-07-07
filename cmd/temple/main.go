// temple — fail the build when a repo drifts past its declared scope.
//
// Usage: temple check [--root .] [--config temple.toml] [--format text|json|sarif]
//
// Exit codes:
//
//	0  in scope (pass)
//	1  scope breach (fail)
//	2  could not evaluate (missing or invalid temple.toml)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goweft/temple/internal"
)

const (
	exitOK     = 0
	exitBreach = 1
	exitNoEval = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("temple", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root to inspect")
	config := fs.String("config", internal.DefaultContract, "path to the scope contract")
	format := fs.String("format", "text", "output format: text, json, or sarif")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: temple check [flags]")
		fmt.Fprintln(os.Stderr, "       temple [flags]")
		fs.PrintDefaults()
	}

	// Allow "temple check [flags]" or "temple [flags]" — consume optional "check" subcommand.
	if len(args) > 0 && args[0] == "check" {
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return exitNoEval
	}

	switch *format {
	case "text", "json", "sarif":
	default:
		fmt.Fprintf(os.Stderr, "temple: unknown format %q (want text, json, or sarif)\n", *format)
		return exitNoEval
	}

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "temple: invalid root %q: %v\n", *root, err)
		return exitNoEval
	}

	configPath := *config
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(absRoot, configPath)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "temple: no scope contract found at %s\n", configPath)
		fmt.Fprintln(os.Stderr, "temple: declare this repo's scope in a temple.toml. See the README.")
		return exitNoEval
	}

	contract, err := internal.LoadContract(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitNoEval
	}

	findings, stats, err := internal.Run(absRoot, contract)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitNoEval
	}

	var output string
	switch *format {
	case "sarif":
		configRel, err := filepath.Rel(absRoot, configPath)
		if err != nil {
			configRel = filepath.Base(configPath)
		}
		output = internal.RenderSARIF(contract, findings, stats, filepath.ToSlash(configRel))
	case "json":
		output = internal.RenderJSON(contract, findings, stats)
	default:
		output = internal.RenderText(contract, findings, stats)
	}
	fmt.Println(output)

	if len(findings) > 0 {
		return exitBreach
	}
	return exitOK
}
