package main

import (
	"log"
	"os"

	"github.com/alecthomas/kong"
	"github.com/winebarrel/tflocalexpand"
)

var version string

func init() {
	log.SetFlags(0)
}

type options struct {
	Dir     string `arg:"" optional:"" default:"." help:"Directory containing *.tf files (default: \".\")."`
	InPlace bool   `short:"i" help:"Write changes back to files instead of stdout."`
	Prune   bool   `short:"p" help:"Expand inside locals blocks and remove local definitions with no remaining references."`
	Eval    bool   `short:"e" help:"Fold direct attribute/index accesses on a substituted local (e.g. local.obj.foo) to a literal when fully evaluatable."`
	Verbose bool   `short:"v" help:"Verbose logging."`
	Version kong.VersionFlag
}

func parseArgs() *options {
	opts := &options{}
	parser := kong.Must(opts,
		kong.Name("tflocalexpand"),
		kong.Description("Expand local.<name> references in Terraform .tf files."),
		kong.Vars{"version": version},
	)
	parser.Model.HelpFlag.Help = "Show help."
	if _, err := parser.Parse(os.Args[1:]); err != nil {
		parser.FatalIfErrorf(err)
	}
	return opts
}

func main() {
	opts := parseArgs()
	e := tflocalexpand.NewExpander(opts.Dir)
	e.Verbose = opts.Verbose
	e.Prune = opts.Prune
	e.Eval = opts.Eval
	if err := e.Expand(opts.InPlace); err != nil {
		log.Fatalf("error: %v", err)
	}
}
