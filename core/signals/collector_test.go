package signals

import (
	"context"
	"testing"
)

func TestGopsutilCollectorMachineReportsTotalMemory(t *testing.T) {
	machine, err := (&GopsutilCollector{}).Machine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if machine.Memory.TotalBytes == 0 {
		t.Fatalf("machine = %#v", machine)
	}
}
