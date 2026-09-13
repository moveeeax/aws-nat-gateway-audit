package audit

import (
	"context"
	"math"
	"testing"
	"time"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestMonthlyCost(t *testing.T) {
	tests := []struct {
		name string
		p    Pricing
		gb   float64
		want float64
	}{
		{"idle default", DefaultPricing(), 0, 0.045 * 730},
		{"1000gb default", DefaultPricing(), 1000, 0.045*730 + 1000*0.045},
		{"negative gb clamps", DefaultPricing(), -50, 0.045 * 730},
		{"eu-central", Pricing{0.052, 0.052}, 100, 0.052*730 + 100*0.052},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MonthlyCost(tt.p, tt.gb); !almost(got, tt.want) {
				t.Fatalf("MonthlyCost=%v want %v", got, tt.want)
			}
		})
	}
}

func TestProjectMonthlyGB(t *testing.T) {
	tests := []struct {
		obs  float64
		days int
		want float64
	}{
		{140, 14, 300}, // 10 GB/day -> 300/mo
		{0, 14, 0},
		{100, 0, 0}, // guard divide-by-zero
		{100, -5, 0},
		{30, 30, 30},
	}
	for _, tt := range tests {
		if got := ProjectMonthlyGB(tt.obs, tt.days); !almost(got, tt.want) {
			t.Fatalf("ProjectMonthlyGB(%v,%v)=%v want %v", tt.obs, tt.days, got, tt.want)
		}
	}
}

func TestClassifyIdle(t *testing.T) {
	tests := []struct {
		name       string
		gb, conns  float64
		gbF, connF float64
		want       IdleVerdict
	}{
		{"dead", 0, 0, 1, 5, Idle},
		{"low bytes but connections", 0.2, 40, 1, 5, Active},
		{"bytes but no conns", 500, 0, 1, 5, Active},
		{"exactly at floor is active", 1, 5, 1, 5, Active},
		{"just under both", 0.99, 4.9, 1, 5, Idle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyIdle(tt.gb, tt.conns, tt.gbF, tt.connF); got != tt.want {
				t.Fatalf("ClassifyIdle=%v want %v", got, tt.want)
			}
		})
	}
}

func TestPriceTableFallback(t *testing.T) {
	pt := BuiltinPriceTable()
	if got := pt.For("eu-central-1").HourlyUSD; !almost(got, 0.052) {
		t.Fatalf("eu-central-1 hourly=%v want 0.052", got)
	}
	// Unknown region falls back to us-east-1 default.
	if got := pt.For("mars-east-9"); !almost(got.HourlyUSD, 0.045) || !almost(got.PerGBUSD, 0.045) {
		t.Fatalf("fallback=%v want default", got)
	}
}

func TestEvaluateAndTotalWaste(t *testing.T) {
	params := Params{LookbackDays: 30, IdleGBFloor: 1, IdleConnFloor: 5}
	idle := Gateway{ID: "nat-idle", Region: "us-east-1", Metrics: Metrics{ObservedGB: 0.1, PeakActiveConnections: 1}}
	busy := Gateway{ID: "nat-busy", Region: "us-east-1", Metrics: Metrics{ObservedGB: 300, PeakActiveConnections: 900}}

	fi := Evaluate(idle, DefaultPricing(), params)
	if fi.Verdict != Idle {
		t.Fatalf("idle gateway not classified idle: %v", fi.Verdict)
	}
	// Idle cost is essentially the hourly baseline.
	if !almost(fi.MonthlyCostUSD, 0.045*730+ProjectMonthlyGB(0.1, 30)*0.045) {
		t.Fatalf("idle cost wrong: %v", fi.MonthlyCostUSD)
	}

	fb := Evaluate(busy, DefaultPricing(), params)
	if fb.Verdict != Active {
		t.Fatalf("busy gateway not active: %v", fb.Verdict)
	}

	waste := TotalWaste([]Finding{fi, fb})
	if !almost(waste, fi.MonthlyCostUSD) {
		t.Fatalf("TotalWaste=%v want only idle cost %v", waste, fi.MonthlyCostUSD)
	}
}

func TestRunOrdersByCostAndUsesProvider(t *testing.T) {
	gws := []Gateway{
		{ID: "nat-cheap", Region: "us-east-1"},
		{ID: "nat-expensive", Region: "us-east-1"},
	}
	metrics := map[string]Metrics{
		"nat-cheap":     {ObservedGB: 0, PeakActiveConnections: 0},
		"nat-expensive": {ObservedGB: 700, PeakActiveConnections: 5000},
	}
	list := func(context.Context) ([]Gateway, error) { return gws, nil }
	fetch := func(_ context.Context, id string, _, _ time.Time, _ int32) (Metrics, error) {
		return metrics[id], nil
	}
	fs, err := Run(context.Background(), list, fetch, BuiltinPriceTable(), Params{LookbackDays: 14, IdleGBFloor: 1, IdleConnFloor: 5}, time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 {
		t.Fatalf("want 2 findings, got %d", len(fs))
	}
	if fs[0].Gateway.ID != "nat-expensive" {
		t.Fatalf("expected cost-desc ordering, got %s first", fs[0].Gateway.ID)
	}
	if fs[1].Verdict != Idle {
		t.Fatalf("cheap gateway should be idle, got %v", fs[1].Verdict)
	}
}
