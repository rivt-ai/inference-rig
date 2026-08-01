package cmd

import (
	"context"
	"encoding/json"
	"errors"
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

// healthClient adapts the generated control client to the one call doctor
// makes, so core/doctor does not depend on the RPC transport.
type healthClient struct{ client controlv1connect.ControlServiceClient }

func (h healthClient) Health(ctx context.Context) error {
	_, err := h.client.Health(ctx, &controlv1.HealthRequest{})
	return err
}

func doctorCommand(validate func(context.Context) error) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose this InferenceRig installation",
		Long: "Check configuration, permissions and daemon state, and report how to fix\n" +
			"what is wrong. Runs without a control daemon: that is usually why you are\n" +
			"running it. It only reads — it never starts, stops or repairs anything.",
		Args: cobra.NoArgs,
		// The report is the output; a usage dump after it buries the findings.
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(command *cobra.Command, _ []string) error {
			report, err := doctor.NewRunner(doctor.Options{
				ValidateConfig: validate,
				DialControl:    dialHealth,
			}).Run(command.Context())
			if err != nil {
				return err
			}
			if err := writeReport(command, report, asJSON); err != nil {
				return err
			}
			if report.Worst() == doctor.StatusFail {
				return errDoctorFailed
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return command
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
