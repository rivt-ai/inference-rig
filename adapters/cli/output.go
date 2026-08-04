package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Output modes. The zero value is auto, which is what an unset flag parses to,
// so "no flag" and "decide from the terminal" are the same state rather than
// two states that can disagree.
const (
	outputAuto = ""
	outputJSON = "json"
	outputText = "text"
)

// OutputFlagName is exported so the root command can register the flag without
// hardcoding a string that this package then has to look up by the same name.
const OutputFlagName = "output"

// RegisterOutputFlag adds --output to root as a persistent flag, making it
// available on every subcommand and in either position (`infr --output json
// health` and `infr health --output json` both work).
//
// It lives here, next to the code that reads the flag, so the writer and the
// reader of the value cannot drift apart.
func RegisterOutputFlag(root *cobra.Command) {
	root.PersistentFlags().String(OutputFlagName, outputAuto,
		"output format: json, text, or unset to pick by whether stdout is a terminal")
	_ = root.RegisterFlagCompletionFunc(OutputFlagName,
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{outputJSON, outputText}, cobra.ShellCompDirectiveNoFileComp
		})
}

// outputMode reads the flag, tolerating its absence. A test that builds a root
// command without RegisterOutputFlag gets auto, which is the same answer as an
// unset flag — the flag being missing is not an error worth failing a command
// over.
func outputMode(command *cobra.Command) string {
	mode, err := command.Flags().GetString(OutputFlagName)
	if err != nil {
		return outputAuto
	}
	return mode
}

// validateOutput rejects a misspelled mode up front. Without it, `--output
// jsonn` silently falls through to auto and the operator gets text they then
// try to pipe into jq.
func validateOutput(command *cobra.Command) error {
	switch mode := outputMode(command); mode {
	case outputAuto, outputJSON, outputText:
		return nil
	default:
		return fmt.Errorf("unknown --output %q: want %q or %q", mode, outputJSON, outputText)
	}
}
