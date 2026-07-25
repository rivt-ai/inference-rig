package main

import (
	"inferencerig/cmd"
	"os"
)

func main() {
	if err := cmd.Execute(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
