package audit

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"text/tabwriter"
)

// SortByCostDesc orders findings most-expensive first for stable, useful output.
func SortByCostDesc(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		return fs[i].MonthlyCostUSD > fs[j].MonthlyCostUSD
	})
}

// RenderTable writes a human-readable table of findings.
func RenderTable(w io.Writer, fs []Finding) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAT_GATEWAY\tREGION\tVPC\tVERDICT\tGB/MO\t$/MO")
	for _, f := range fs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%.1f\t%.2f\n",
			f.Gateway.ID, f.Gateway.Region, f.Gateway.VPCID, f.Verdict, f.MonthlyGB, f.MonthlyCostUSD)
	}
	fmt.Fprintf(tw, "\nIDLE waste: $%.2f/mo across %d gateway(s)\n", TotalWaste(fs), countIdle(fs))
	return tw.Flush()
}

func countIdle(fs []Finding) int {
	n := 0
	for _, f := range fs {
		if f.Verdict == Idle {
			n++
		}
	}
	return n
}

// jsonFinding is the flattened wire form so JSON stays stable and readable.
type jsonFinding struct {
	ID             string  `json:"id"`
	Region         string  `json:"region"`
	VPCID          string  `json:"vpc_id"`
	SubnetID       string  `json:"subnet_id"`
	State          string  `json:"state"`
	Verdict        string  `json:"verdict"`
	MonthlyGB      float64 `json:"monthly_gb"`
	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
}

type jsonReport struct {
	Findings      []jsonFinding `json:"findings"`
	TotalWasteUSD float64       `json:"total_waste_usd"`
	IdleCount     int           `json:"idle_count"`
}

// RenderJSON writes findings and the waste summary as indented JSON.
func RenderJSON(w io.Writer, fs []Finding) error {
	rep := jsonReport{TotalWasteUSD: TotalWaste(fs), IdleCount: countIdle(fs)}
	for _, f := range fs {
		rep.Findings = append(rep.Findings, jsonFinding{
			ID: f.Gateway.ID, Region: f.Gateway.Region, VPCID: f.Gateway.VPCID,
			SubnetID: f.Gateway.SubnetID, State: f.Gateway.State, Verdict: string(f.Verdict),
			MonthlyGB: round1(f.MonthlyGB), MonthlyCostUSD: round2(f.MonthlyCostUSD),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// RenderCSV writes findings as CSV (no summary row — friendlier to pipelines).
func RenderCSV(w io.Writer, fs []Finding) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "region", "vpc_id", "subnet_id", "state", "verdict", "monthly_gb", "monthly_cost_usd"}); err != nil {
		return err
	}
	for _, f := range fs {
		rec := []string{
			f.Gateway.ID, f.Gateway.Region, f.Gateway.VPCID, f.Gateway.SubnetID, f.Gateway.State,
			string(f.Verdict),
			strconv.FormatFloat(round1(f.MonthlyGB), 'f', 1, 64),
			strconv.FormatFloat(round2(f.MonthlyCostUSD), 'f', 2, 64),
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }
