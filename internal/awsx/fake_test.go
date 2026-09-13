package awsx

import (
	"context"
	"testing"
	"time"

	"github.com/moveeeax/aws-nat-gateway-audit/internal/audit"
)

func TestDemoProviderClassifies(t *testing.T) {
	fp := DemoProvider("us-east-1")
	fs, err := audit.Run(context.Background(),
		fp.ListNATGateways, fp.GatewayMetrics,
		audit.BuiltinPriceTable(),
		audit.Params{LookbackDays: 14, IdleGBFloor: 1, IdleConnFloor: 5},
		time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 4 {
		t.Fatalf("want 4 findings, got %d", len(fs))
	}
	var idle int
	for _, f := range fs {
		if f.Verdict == audit.Idle {
			idle++
		}
	}
	if idle != 2 { // the 0.3GB/2conn and the dead 0/0 gateways
		t.Fatalf("want 2 idle gateways in demo set, got %d", idle)
	}
}

func TestFakeError(t *testing.T) {
	f := &Fake{Err: context.DeadlineExceeded}
	if _, err := f.ListNATGateways(context.Background()); err == nil {
		t.Fatal("expected error from fake")
	}
}
