package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sampleFindings() []Finding {
	return []Finding{
		{Gateway: Gateway{ID: "nat-a", Region: "us-east-1", VPCID: "vpc-1"}, MonthlyGB: 0, MonthlyCostUSD: 32.85, Verdict: Idle},
		{Gateway: Gateway{ID: "nat-b", Region: "us-east-1", VPCID: "vpc-1"}, MonthlyGB: 500, MonthlyCostUSD: 55.35, Verdict: Active},
	}
}

func TestRenderTable(t *testing.T) {
	var b bytes.Buffer
	if err := RenderTable(&b, sampleFindings()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"nat-a", "IDLE", "IDLE waste: $32.85"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	var b bytes.Buffer
	if err := RenderJSON(&b, sampleFindings()); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Findings      []map[string]any `json:"findings"`
		TotalWasteUSD float64          `json:"total_waste_usd"`
		IdleCount     int              `json:"idle_count"`
	}
	if err := json.Unmarshal(b.Bytes(), &rep); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, b.String())
	}
	if rep.IdleCount != 1 {
		t.Fatalf("idle_count=%d want 1", rep.IdleCount)
	}
	if rep.TotalWasteUSD != 32.85 {
		t.Fatalf("total_waste_usd=%v want 32.85", rep.TotalWasteUSD)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(rep.Findings))
	}
}

func TestRenderCSV(t *testing.T) {
	var b bytes.Buffer
	if err := RenderCSV(&b, sampleFindings()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 3 { // header + 2 rows
		t.Fatalf("want 3 csv lines, got %d:\n%s", len(lines), b.String())
	}
	if !strings.HasPrefix(lines[0], "id,region,vpc_id") {
		t.Fatalf("unexpected header: %s", lines[0])
	}
}
