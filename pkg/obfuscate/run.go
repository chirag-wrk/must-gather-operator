package obfuscate

import (
	goflag "flag"
	"fmt"
	"os"

	mgclean "github.com/openshift/must-gather-clean/pkg/cli"
	_ "go.uber.org/automaxprocs"
	"k8s.io/klog/v2"
)

// Run executes the obfuscate subcommand. Args are everything after "obfuscate" on the command line.
// Returns an exit code suitable for os.Exit.
func Run(args []string) int {
	fs := goflag.NewFlagSet("obfuscate", goflag.ExitOnError)
	fs.SetOutput(os.Stderr)
	klog.InitFlags(fs)

	input := fs.String("input", "", "directory containing the must-gather bundle to obfuscate")
	output := fs.String("output", "", "directory where the obfuscated bundle is written")
	config := fs.String("config", DefaultConfigPath, "path to the must-gather-clean configuration file")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse obfuscate flags: %v\n", err)
		return 1
	}

	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "obfuscate requires --input and --output")
		return 1
	}

	if err := mgclean.Run(*config, *input, *output, true, *output, WorkerCount); err != nil {
		fmt.Fprintf(os.Stderr, "obfuscation failed: %v\n", err)
		return 1
	}

	return 0
}
