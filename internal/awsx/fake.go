package awsx

import (
	"context"
	"time"

	"github.com/moveeeax/aws-nat-gateway-audit/internal/audit"
)

// Fake is an in-memory Provider used by tests and the --demo mode. It serves a
// fixed set of gateways and their metrics without touching AWS.
type Fake struct {
	Gateways []audit.Gateway
	// MetricsByID overrides metrics per gateway id; falls back to the metrics
	// embedded in the Gateway value when absent.
	MetricsByID map[string]audit.Metrics
	Err         error
}

// ListNATGateways returns the fake's gateway set.
func (f *Fake) ListNATGateways(_ context.Context) ([]audit.Gateway, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Gateways, nil
}

// GatewayMetrics returns the override metrics for an id, or the gateway's own.
func (f *Fake) GatewayMetrics(_ context.Context, natID string, _, _ time.Time, _ int32) (audit.Metrics, error) {
	if f.Err != nil {
		return audit.Metrics{}, f.Err
	}
	if f.MetricsByID != nil {
		if m, ok := f.MetricsByID[natID]; ok {
			return m, nil
		}
	}
	for _, g := range f.Gateways {
		if g.ID == natID {
			return g.Metrics, nil
		}
	}
	return audit.Metrics{}, nil
}

// DemoProvider returns a Fake seeded with a realistic mix of idle and active
// gateways so `--demo` shows something useful without AWS credentials.
func DemoProvider(region string) *Fake {
	return &Fake{
		Gateways: []audit.Gateway{
			{ID: "nat-0aa11idle0000001", VPCID: "vpc-demo1", SubnetID: "subnet-a", Region: region, State: "available",
				Metrics: audit.Metrics{ObservedGB: 0.3, PeakActiveConnections: 2}},
			{ID: "nat-0bb22busy0000002", VPCID: "vpc-demo1", SubnetID: "subnet-b", Region: region, State: "available",
				Metrics: audit.Metrics{ObservedGB: 812.5, PeakActiveConnections: 4100}},
			{ID: "nat-0cc33s3only00003", VPCID: "vpc-demo2", SubnetID: "subnet-c", Region: region, State: "available",
				Metrics: audit.Metrics{ObservedGB: 240.0, PeakActiveConnections: 180}},
			{ID: "nat-0dd44dead000004", VPCID: "vpc-demo2", SubnetID: "subnet-d", Region: region, State: "available",
				Metrics: audit.Metrics{ObservedGB: 0.0, PeakActiveConnections: 0}},
		},
	}
}
