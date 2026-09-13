package audit

import (
	"context"
	"time"
)

// MetricsFetcher fetches activity metrics for one NAT Gateway. It mirrors the
// awsx.Provider method so audit stays decoupled from the AWS package.
type MetricsFetcher func(ctx context.Context, natID string, start, end time.Time, periodSec int32) (Metrics, error)

// GatewayLister lists NAT Gateways in a region.
type GatewayLister func(ctx context.Context) ([]Gateway, error)

// Run enumerates gateways, fetches metrics for each, and evaluates findings.
// It is provider-agnostic: callers pass the two closures (real or fake).
func Run(ctx context.Context, list GatewayLister, fetch MetricsFetcher, prices Pricer, params Params, end time.Time) ([]Finding, error) {
	gws, err := list(ctx)
	if err != nil {
		return nil, err
	}
	start := end.Add(-time.Duration(params.LookbackDays) * 24 * time.Hour)
	period := int32(3600) // hourly datapoints
	var findings []Finding
	for _, g := range gws {
		m, err := fetch(ctx, g.ID, start, end, period)
		if err != nil {
			return nil, err
		}
		g.Metrics = m
		findings = append(findings, Evaluate(g, prices.For(g.Region), params))
	}
	SortByCostDesc(findings)
	return findings, nil
}
