// Package audit contains the pure cost/idle logic and the orchestration that
// turns AWS API responses into a set of NAT Gateway findings.
package audit

import "fmt"

// HoursPerMonth is the AWS billing convention: 730 hours in a month.
const HoursPerMonth = 730.0

// Pricing holds the two NAT Gateway charge components for a region.
// Rates are in USD.
type Pricing struct {
	// HourlyUSD is the per-NAT-Gateway hourly charge (just for existing).
	HourlyUSD float64
	// PerGBUSD is the data-processing charge per GB routed through the gateway.
	PerGBUSD float64
}

// DefaultPricing returns the us-east-1 NAT Gateway rates, used when a region
// is not present in the price table.
func DefaultPricing() Pricing {
	return Pricing{HourlyUSD: 0.045, PerGBUSD: 0.045}
}

// Pricer resolves NAT Gateway pricing for a region. Both the region-aware
// PriceTable and a flat-rate override implement it.
type Pricer interface {
	For(region string) Pricing
}

// PriceTable maps an AWS region to its NAT Gateway pricing. Lookups fall back
// to DefaultPricing for regions that are not listed.
type PriceTable map[string]Pricing

// For returns the pricing for a region, falling back to us-east-1 rates.
func (t PriceTable) For(region string) Pricing {
	if p, ok := t[region]; ok {
		return p
	}
	return DefaultPricing()
}

// BuiltinPriceTable is a small static table covering common regions. Prices
// track public AWS list prices at time of writing; use --price-* flags to
// override for regions or dates not represented here.
func BuiltinPriceTable() PriceTable {
	return PriceTable{
		"us-east-1":      {0.045, 0.045},
		"us-east-2":      {0.045, 0.045},
		"us-west-1":      {0.048, 0.048},
		"us-west-2":      {0.045, 0.045},
		"eu-west-1":      {0.048, 0.048},
		"eu-central-1":   {0.052, 0.052},
		"eu-north-1":     {0.048, 0.048},
		"ap-southeast-1": {0.059, 0.059},
		"ap-southeast-2": {0.059, 0.059},
		"ap-northeast-1": {0.062, 0.062},
		"ap-south-1":     {0.056, 0.056},
	}
}

// MonthlyCost estimates the full monthly cost of a NAT Gateway given the
// pricing and the GB it processed over a 30-day-equivalent month. The hourly
// component is always charged; the data component scales with processed GB.
func MonthlyCost(p Pricing, processedGBPerMonth float64) float64 {
	if processedGBPerMonth < 0 {
		processedGBPerMonth = 0
	}
	return p.HourlyUSD*HoursPerMonth + processedGBPerMonth*p.PerGBUSD
}

// ProjectMonthlyGB extrapolates the GB processed over a lookback window of
// lookbackDays to a 30-day month. A zero or negative window returns 0 to avoid
// a divide-by-zero.
func ProjectMonthlyGB(observedGB float64, lookbackDays int) float64 {
	if lookbackDays <= 0 {
		return 0
	}
	return observedGB / float64(lookbackDays) * 30.0
}

// IdleVerdict classifies a NAT Gateway's activity.
type IdleVerdict string

const (
	// Idle means neither bytes nor connections crossed the activity floor.
	Idle IdleVerdict = "IDLE"
	// Active means the gateway carried meaningful traffic.
	Active IdleVerdict = "ACTIVE"
)

// ClassifyIdle decides whether a gateway is idle over the lookback window.
// A gateway is idle when it processed fewer than idleGBFloor GB *and* saw
// fewer than idleConnFloor peak active connections. Requiring both avoids
// flagging a low-throughput-but-live gateway as idle.
func ClassifyIdle(observedGB float64, peakActiveConns float64, idleGBFloor float64, idleConnFloor float64) IdleVerdict {
	if observedGB < idleGBFloor && peakActiveConns < idleConnFloor {
		return Idle
	}
	return Active
}

// Metrics is the observed CloudWatch activity for one NAT Gateway over the
// lookback window.
type Metrics struct {
	// ObservedGB is total BytesOutToDestination + BytesOutToSource converted
	// to GB over the whole window.
	ObservedGB float64
	// PeakActiveConnections is the max ActiveConnectionCount seen in the window.
	PeakActiveConnections float64
}

// Gateway is a NAT Gateway plus everything we need to price and classify it.
type Gateway struct {
	ID       string
	VPCID    string
	SubnetID string
	Region   string
	State    string
	Metrics  Metrics
}

// Finding is the audit result for a single NAT Gateway.
type Finding struct {
	Gateway        Gateway
	MonthlyGB      float64
	MonthlyCostUSD float64
	Verdict        IdleVerdict
}

// String renders a finding as a compact one-liner (handy in logs/tests).
func (f Finding) String() string {
	return fmt.Sprintf("%s %s %s $%.2f/mo (%.1f GB/mo)", f.Gateway.ID, f.Gateway.Region, f.Verdict, f.MonthlyCostUSD, f.MonthlyGB)
}

// Params bundles the tunable audit thresholds.
type Params struct {
	LookbackDays  int
	IdleGBFloor   float64
	IdleConnFloor float64
}

// Evaluate turns a gateway plus pricing into a Finding using pure math.
func Evaluate(g Gateway, p Pricing, params Params) Finding {
	monthlyGB := ProjectMonthlyGB(g.Metrics.ObservedGB, params.LookbackDays)
	return Finding{
		Gateway:        g,
		MonthlyGB:      monthlyGB,
		MonthlyCostUSD: MonthlyCost(p, monthlyGB),
		Verdict:        ClassifyIdle(g.Metrics.ObservedGB, g.Metrics.PeakActiveConnections, params.IdleGBFloor, params.IdleConnFloor),
	}
}

// TotalWaste sums the monthly cost of every IDLE gateway in the set — the
// number most people actually care about.
func TotalWaste(findings []Finding) float64 {
	var w float64
	for _, f := range findings {
		if f.Verdict == Idle {
			w += f.MonthlyCostUSD
		}
	}
	return w
}
