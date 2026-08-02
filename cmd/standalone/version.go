package standalone

import (
	"encoding/json"
	"fmt"

	"inferencerig/internal/buildinfo"

	"github.com/spf13/cobra"
)

func VersionCommand() *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "version",
		Short: "Print build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
					"version":     buildinfo.Version,
					"commit":      buildinfo.Commit,
					"commit_time": buildinfo.CommitTime,
				})
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s (commit %s, %s)\n",
				cmd.Root().Name(), buildinfo.Version, buildinfo.Commit, buildinfo.CommitTime)
			return err
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print JSON output")
	return command
}
