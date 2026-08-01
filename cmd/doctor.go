package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"inferencerig/core/doctor"
	"inferencerig/core/rpc"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// errDoctorFailed makes a failing diagnosis a non-zero exit without repeating
// the report that was just printed.
var errDoctorFailed = errors.New("doctor found problems")

// errNothingToFix reports --fix with no automatically repairable failure.
var errNothingToFix = errors.New("nothing here can be repaired automatically")

// healthClient adapts the generated control client to the one call doctor
// makes, so core/doctor does not depend on the RPC transport.
type healthClient struct {
	client controlv1connect.ControlServiceClient
}

func (h healthClient) Health(ctx context.Context) error {
	_, err := h.client.Health(ctx, &controlv1.HealthRequest{})
	return err
}

type doctorFlags struct {
	asJSON  bool
	fix     bool
	fixWith string
}

func doctorCommand(validate func(context.Context) error) *cobra.Command {
	var flags doctorFlags
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose this InferenceRig installation",
		Long: "Check configuration, permissions and daemon state, and report how to fix\n" +
			"what is wrong. Runs without a control daemon: that is usually why you are\n" +
			"running it. Reads only, unless you pass --fix or --fix-with.",
		Args: cobra.NoArgs,
		// The report is the output; a usage dump after it buries the findings.
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return runDoctor(command, validate, flags)
		},
	}
	command.Flags().BoolVar(&flags.asJSON, "json", false, "print the report as JSON")
	command.Flags().BoolVar(&flags.fix, "fix", false, "choose a repair interactively")
	command.Flags().StringVar(&flags.fixWith, "fix-with", "",
		"apply a named repair without prompting ("+strings.Join(doctor.RemedyIDs(), ", ")+")")
	command.MarkFlagsMutuallyExclusive("fix", "fix-with")
	// Repairing writes to the config file and prompts; neither belongs in a
	// stream something else is parsing.
	command.MarkFlagsMutuallyExclusive("json", "fix")
	command.MarkFlagsMutuallyExclusive("json", "fix-with")
	return command
}

func runDoctor(command *cobra.Command, validate func(context.Context) error, flags doctorFlags) error {
	report, err := diagnose(command, validate)
	if err != nil {
		return err
	}
	if flags.fix || flags.fixWith != "" {
		return fixAndRediagnose(command, validate, report, flags)
	}
	if err := writeReport(command, report, flags.asJSON); err != nil {
		return err
	}
	if report.Worst() == doctor.StatusFail {
		return errDoctorFailed
	}
	return nil
}

func diagnose(command *cobra.Command, validate func(context.Context) error) (doctor.Report, error) {
	return doctor.NewRunner(doctor.Options{
		ValidateConfig: validate,
		DialControl:    dialHealth,
	}).Run(command.Context())
}

// fixAndRediagnose applies one remedy and reports the installation as it
// stands afterwards, so the operator sees the result rather than being told a
// write succeeded and left to check for themselves.
func fixAndRediagnose(
	command *cobra.Command, validate func(context.Context) error, report doctor.Report, flags doctorFlags,
) error {
	if err := report.WriteText(command.OutOrStdout()); err != nil {
		return err
	}
	remedy, err := chooseRemedy(command, report, flags)
	if err != nil {
		return err
	}
	result, err := doctor.Apply(command.Context(), report.ConfigPath, remedy)
	if err != nil {
		return err
	}
	if !result.Changed {
		command.Printf("\n%s was already applied; nothing to change.\n", result.RemedyID)
		return nil
	}
	command.Printf("\nApplied %s to %s\n  previous version saved to %s\n\n",
		result.RemedyID, report.ConfigPath, result.BackupPath)

	after, err := diagnose(command, validate)
	if err != nil {
		return err
	}
	if err := after.WriteText(command.OutOrStdout()); err != nil {
		return err
	}
	if after.Worst() == doctor.StatusFail {
		return errDoctorFailed
	}
	return nil
}

func chooseRemedy(command *cobra.Command, report doctor.Report, flags doctorFlags) (string, error) {
	if flags.fixWith != "" {
		return flags.fixWith, nil
	}
	fixable := report.Fixable()
	if len(fixable) == 0 {
		return "", errNothingToFix
	}
	remedy, err := doctor.SelectRemedy(command.Context(), command.InOrStdin(), command.OutOrStdout(), fixable)
	switch {
	case errors.Is(err, doctor.ErrNotInteractive):
		// Nobody can be asked, so leave behind everything needed to decide and
		// act without doctor. Choosing a security posture unattended is not
		// something a diagnostic gets to do.
		if writeErr := doctor.WriteRemedyOptions(command.ErrOrStderr(), fixable); writeErr != nil {
			return "", writeErr
		}
		return "", err
	case err != nil:
		return "", err
	}
	return remedy, nil
}

func writeReport(command *cobra.Command, report doctor.Report, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(command.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	return report.WriteText(command.OutOrStdout())
}

func dialHealth(socket string) (doctor.HealthChecker, error) {
	client, err := rpc.DialControl(socket, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return healthClient{client: client}, nil
}
