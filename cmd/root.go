// Package cmd wires the CLI flags to the audit engine.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/moveeeax/aws-nat-gateway-audit/internal/audit"
	"github.com/moveeeax/aws-nat-gateway-audit/internal/awsx"
)

// Version is set at build time via -ldflags; defaults to "dev".
var Version = "dev"

type options struct {
	region        string
	allRegions    bool
	regions       []string
	days          int
	output        string
	threshold     float64
	idleGBFloor   float64
	idleConnFloor float64
	priceHourly   float64
	pricePerGB    float64
	demo          bool
}

// Execute runs the root command and returns the process exit code.
func Execute() int {
	var o options
	root := &cobra.Command{
		Use:   "aws-nat-gateway-audit",
		Short: "Find idle/over-provisioned AWS NAT Gateways and estimate monthly waste",
		Long: "aws-nat-gateway-audit inventories NAT Gateways, attributes monthly cost " +
			"(hourly + data processing), and flags idle ones for cleanup or VPC-endpoint remediation.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), o, cmd.OutOrStdout())
		},
		Version: Version,
	}

	f := root.Flags()
	f.StringVar(&o.region, "region", "", "AWS region to audit (default: from AWS config/env)")
	f.BoolVar(&o.allRegions, "all-regions", false, "audit a built-in list of common regions")
	f.StringSliceVar(&o.regions, "regions", nil, "explicit comma-separated region list (overrides --region/--all-regions)")
	f.IntVar(&o.days, "days", 14, "lookback window in days for CloudWatch metrics")
	f.StringVar(&o.output, "output", "table", "output format: table|json|csv")
	f.Float64Var(&o.threshold, "threshold", 0, "exit non-zero if total IDLE waste $/mo exceeds this (0 = never fail)")
	f.Float64Var(&o.idleGBFloor, "idle-gb-floor", 1.0, "GB/window below which a gateway is considered idle")
	f.Float64Var(&o.idleConnFloor, "idle-conn-floor", 5, "peak active connections below which a gateway is considered idle")
	f.Float64Var(&o.priceHourly, "price-hourly", 0, "override hourly USD rate for all regions (0 = use price table)")
	f.Float64Var(&o.pricePerGB, "price-per-gb", 0, "override per-GB USD rate for all regions (0 = use price table)")
	f.BoolVar(&o.demo, "demo", false, "run against built-in fake data (no AWS credentials needed)")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return exitCode
}

// exitCode is set by run() so a threshold breach returns 2 without treating it
// as a hard error.
var exitCode int

func commonRegions() []string {
	return []string{"us-east-1", "us-east-2", "us-west-2", "eu-west-1", "eu-central-1", "ap-southeast-1"}
}

func run(ctx context.Context, o options, out io.Writer) error {
	var prices audit.Pricer = audit.BuiltinPriceTable()
	if o.priceHourly > 0 || o.pricePerGB > 0 {
		p := audit.DefaultPricing()
		if o.priceHourly > 0 {
			p.HourlyUSD = o.priceHourly
		}
		if o.pricePerGB > 0 {
			p.PerGBUSD = o.pricePerGB
		}
		// Force every region to the override by wrapping the table.
		prices = overrideTable{p}
	}

	params := audit.Params{LookbackDays: o.days, IdleGBFloor: o.idleGBFloor, IdleConnFloor: o.idleConnFloor}
	end := time.Now().UTC()

	regions := resolveRegions(o)

	var all []audit.Finding
	for _, region := range regions {
		var list audit.GatewayLister
		var fetch audit.MetricsFetcher
		if o.demo {
			fp := awsx.DemoProvider(region)
			list, fetch = fp.ListNATGateways, fp.GatewayMetrics
		} else {
			p, err := awsx.NewSDKProvider(ctx, region)
			if err != nil {
				return fmt.Errorf("region %s: %w", region, err)
			}
			list, fetch = p.ListNATGateways, p.GatewayMetrics
		}
		fs, err := audit.Run(ctx, list, fetch, prices, params, end)
		if err != nil {
			return fmt.Errorf("region %s: %w", region, err)
		}
		all = append(all, fs...)
	}
	audit.SortByCostDesc(all)

	if err := render(out, o.output, all); err != nil {
		return err
	}

	if o.threshold > 0 && audit.TotalWaste(all) > o.threshold {
		exitCode = 2
	}
	return nil
}

func resolveRegions(o options) []string {
	if len(o.regions) > 0 {
		return o.regions
	}
	if o.allRegions {
		return commonRegions()
	}
	if o.region != "" {
		return []string{o.region}
	}
	if v := os.Getenv("AWS_REGION"); v != "" {
		return []string{v}
	}
	if v := os.Getenv("AWS_DEFAULT_REGION"); v != "" {
		return []string{v}
	}
	return []string{"us-east-1"}
}

func render(out io.Writer, format string, fs []audit.Finding) error {
	switch format {
	case "json":
		return audit.RenderJSON(out, fs)
	case "csv":
		return audit.RenderCSV(out, fs)
	case "table", "":
		return audit.RenderTable(out, fs)
	default:
		return fmt.Errorf("unknown output format %q (want table|json|csv)", format)
	}
}

// overrideTable forces a single Pricing for every region.
type overrideTable struct{ p audit.Pricing }

func (t overrideTable) For(string) audit.Pricing { return t.p }
