package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/antonikliment/tuikit"
	"github.com/spf13/cobra"

	"inferencerig/core/modeldownload"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	"inferencerig/internal/style"
)

// pollInterval is how often the bar asks the daemon where the download is.
// There is no WatchModelDownload stream, so polling is the only option; twice
// a second is frequent enough that the bar looks live and rare enough that it
// is invisible next to the transfer itself.
const pollInterval = 500 * time.Millisecond

// barWidth is the meter's width in cells, leaving room on an 80-column
// terminal for the profile name, percentage and byte counts beside it.
const barWidth = 30

// downloadCommand starts a model download and, on a terminal, stays to draw
// its progress.
//
// The blocking behaviour is deliberately limited to the interactive case. A
// download takes minutes, and a script that calls this expects the old
// fire-and-forget contract, so anything that is not a human at a terminal —
// a pipe, --output json, or an explicit --detach — gets the response printed
// and the command returns, exactly as before.
func downloadCommand(dial dialer) *cobra.Command {
	var socket string
	var detach bool
	command := &cobra.Command{
		Use: "download <profile>", Short: "Download a profile's model",
		Args: cobra.ExactArgs(1), ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(command *cobra.Command, args []string) error {
			if err := validateOutput(command); err != nil {
				return err
			}
			client, err := dial(resolveSocket(socket), dialTimeout())
			if err != nil {
				return err
			}
			ctx := command.Context()
			started, err := client.StartModelDownload(ctx, &controlv1.StartModelDownloadRequest{Profile: args[0]})
			if err != nil {
				return err
			}
			if detach || !renderText(command) {
				return printProto(command, started)
			}
			return followDownload(ctx, command, client, started.GetDownload())
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "control Unix socket")
	command.Flags().BoolVar(&detach, "detach", false,
		"Print the download's id and return instead of waiting for it to finish")
	return command
}

// followDownload draws the bar until the job reaches a terminal state, then
// reports the outcome as the command's error or final line.
func followDownload(ctx context.Context, command *cobra.Command, client controlv1connect.ControlServiceClient, job *controlv1.ModelDownload) error {
	if isTerminalState(job.GetState()) {
		return reportDownload(command, job)
	}
	// SilenceUsage: past this point every error is about the transfer, and a
	// usage dump under "connection reset" helps nobody.
	command.SilenceUsage = true
	final, err := tea.NewProgram(
		newDownloadModel(ctx, client, job),
		tea.WithContext(ctx),
		tea.WithInput(command.InOrStdin()),
		tea.WithOutput(command.OutOrStdout()),
	).Run()
	if err != nil {
		return err
	}
	model, ok := final.(*downloadModel)
	if !ok {
		return fmt.Errorf("download: unexpected final model %T", final)
	}
	if model.err != nil {
		return model.err
	}
	return reportDownload(command, model.job)
}

// reportDownload writes the one line that survives after the bar is gone. A
// failed download exits non-zero, because a script that drops --detach should
// still be able to test the exit status.
func reportDownload(command *cobra.Command, job *controlv1.ModelDownload) error {
	r := renderer{paint: style.PainterFor(command.OutOrStdout())}
	state := modeldownload.State(job.GetState())
	if state == modeldownload.StateFailed {
		return fmt.Errorf("download %s failed: %s", job.GetId(), job.GetError())
	}
	if !state.Succeeded() {
		command.Printf("%s %s\n", r.status(string(state)), job.GetId())
		return nil
	}
	// Painted from Succeeded rather than through r.status, which would read
	// already_downloaded as an unfamiliar word and warn about it. The file the
	// caller asked for is on disk; that is a success however it got there.
	command.Printf("%s %s → %s\n",
		r.paint(style.SuccessStyle, string(state)), job.GetId(), job.GetTargetPath())
	return nil
}

func isTerminalState(state string) bool {
	return modeldownload.State(state).IsTerminal()
}

type downloadModel struct {
	ctx     context.Context
	client  controlv1connect.ControlServiceClient
	job     *controlv1.ModelDownload
	meter   tuikit.Meter
	spinner spinner.Model
	paint   style.Painter
	// cancelling records that the user pressed ctrl-c and the cancel RPC is in
	// flight, so a second press is not mistaken for the first.
	cancelling bool
	err        error
}

type downloadTickMsg struct{}
type downloadStatusMsg struct {
	job *controlv1.ModelDownload
	err error
}

func newDownloadModel(ctx context.Context, client controlv1connect.ControlServiceClient, job *controlv1.ModelDownload) *downloadModel {
	spin := spinner.New(spinner.WithSpinner(spinner.Dot))
	spin.Style = spin.Style.Foreground(style.Cyan)
	return &downloadModel{
		ctx: ctx, client: client, job: job,
		meter: tuikit.NewMeter(barWidth, style.Green), spinner: spin,
		// The bar only ever runs on a terminal — followDownload is not reached
		// otherwise — so it always paints.
		paint: style.Paint,
	}
}

func (m *downloadModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.poll())
}

func (m *downloadModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// ctrl-c on a download that is still running should stop the download,
		// not just stop watching it. Leaving a job running after the operator
		// interrupted it is the surprising outcome.
		if key := msg.String(); key == "ctrl+c" || key == "q" || key == "esc" {
			return m, m.cancel()
		}
	case downloadTickMsg:
		return m, m.poll()
	case downloadStatusMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.job = msg.job
		if isTerminalState(m.job.GetState()) {
			return m, tea.Quit
		}
		return m, tea.Tick(pollInterval, func(time.Time) tea.Msg { return downloadTickMsg{} })
	}
	spin, cmd := m.spinner.Update(msg)
	m.spinner = spin
	return m, cmd
}

func (m *downloadModel) View() tea.View {
	r := renderer{paint: m.paint}
	label := m.job.GetProfile()
	if label == "" {
		label = m.job.GetId()
	}
	// A plan built from a direct URL does not know its total size, so the
	// daemon reports percent 0 for the whole transfer. Drawing a determinate
	// bar there pins it at 0% while gigabytes land, which reads as a stall.
	// With no total to divide by, the byte counter is the honest progress
	// indicator and the spinner is what shows it is still moving.
	progress := r.paint(style.MutedStyle, "downloading")
	if m.job.GetTotalBytes() > 0 {
		progress = fmt.Sprintf("%s %5.1f%%", m.meter.View(int(m.job.GetPercent())), m.job.GetPercent())
	}
	line := fmt.Sprintf("%s %s %s  %s",
		m.spinner.View(), label, progress, r.paint(style.MutedStyle, transferred(m.job)))
	if m.cancelling {
		line += r.paint(style.WarningStyle, "  cancelling…")
	} else {
		line += r.paint(style.MutedStyle, "  ctrl-c to cancel")
	}
	// Not AltScreen: the bar renders inline so the summary line that follows
	// it stays in the terminal's scrollback like any other command's output.
	return tea.NewView(line + "\n")
}

// transferred renders "4.1 GiB / 6.6 GiB", or just the received count while
// the daemon has not yet reported a total (a multi-file plan does not know its
// size until every manifest is fetched).
func transferred(job *controlv1.ModelDownload) string {
	received := tuikit.FormatBytes(job.GetReceivedBytes())
	if job.GetTotalBytes() <= 0 {
		return received
	}
	return received + " / " + tuikit.FormatBytes(job.GetTotalBytes())
}

func (m *downloadModel) poll() tea.Cmd {
	return func() tea.Msg {
		response, err := m.client.GetModelDownload(m.ctx, &controlv1.GetModelDownloadRequest{Id: m.job.GetId()})
		if err != nil {
			// A cancel the user asked for races the poll that follows it; the
			// job going away then is the expected outcome, not a failure.
			if m.cancelling && errors.Is(err, context.Canceled) {
				return downloadStatusMsg{job: m.job}
			}
			return downloadStatusMsg{err: err}
		}
		return downloadStatusMsg{job: response.GetDownload()}
	}
}

func (m *downloadModel) cancel() tea.Cmd {
	if m.cancelling {
		// Already asked. A second ctrl-c stops watching rather than sending a
		// second cancel, so the operator is never stuck in a UI they cannot
		// leave if the daemon is unresponsive.
		return tea.Quit
	}
	m.cancelling = true
	id := m.job.GetId()
	return func() tea.Msg {
		response, err := m.client.CancelModelDownload(m.ctx, &controlv1.CancelModelDownloadRequest{Id: id})
		if err != nil {
			return downloadStatusMsg{err: err}
		}
		return downloadStatusMsg{job: response.GetDownload()}
	}
}
