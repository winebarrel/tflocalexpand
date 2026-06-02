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
	Dir     string   `arg:"" optional:"" default:"." help:"Directory containing *.tf files (default: \".\")."`
	InPlace bool     `short:"i" help:"Write changes back to files instead of stdout."`
	Prune   bool     `short:"p" help:"Expand inside locals blocks and remove local definitions with no remaining references. With --vars, also drop unused variable blocks that have a default."`
	Eval    bool     `short:"e" help:"Fold expressions that become statically evaluatable after substitution (attribute/index accesses, ternaries with a constant condition, comparison/logical operators with constant operands)."`
	Vars    bool     `help:"Also expand var.<name> references using each variable's default value. Variables without a default are left as 'var.<name>'."`
	Only    []string `xor:"select" help:"Only expand these references; leave the rest as-is. Names must be prefixed with 'local.' or 'var.' (e.g. 'local.region,var.port'). Repeat or comma-separate. Mutually exclusive with --except."`
	Except  []string `xor:"select" help:"Do not expand these references; expand the rest. Names must be prefixed with 'local.' or 'var.'. Repeat or comma-separate. Mutually exclusive with --only."`
	Verbose bool     `short:"v" help:"Verbose logging."`
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
	e.Vars = opts.Vars
	e.Only = opts.Only
	e.Except = opts.Except
	if err := e.Expand(opts.InPlace); err != nil {
		log.Fatalf("error: %v", err)
	}
}
