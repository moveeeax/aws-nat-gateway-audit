// Package awsx wraps the parts of the AWS SDK we need behind a small interface
// so the audit logic can be exercised in tests with fakes instead of real
// network calls.
package awsx

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/moveeeax/aws-nat-gateway-audit/internal/audit"
)

// Provider is the minimal surface the audit needs from AWS. Both the real SDK
// client and the test fake implement it.
type Provider interface {
	// ListNATGateways returns available NAT Gateways in the provider's region.
	ListNATGateways(ctx context.Context) ([]audit.Gateway, error)
	// GatewayMetrics fetches activity metrics for one gateway over the window.
	GatewayMetrics(ctx context.Context, natID string, start, end time.Time, periodSec int32) (audit.Metrics, error)
}

// SDKProvider is the real AWS-backed Provider.
type SDKProvider struct {
	region string
	ec2    *ec2.Client
	cw     *cloudwatch.Client
}

// NewSDKProvider loads default AWS config for the region and returns a Provider.
func NewSDKProvider(ctx context.Context, region string) (*SDKProvider, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &SDKProvider{region: region, ec2: ec2.NewFromConfig(cfg), cw: cloudwatch.NewFromConfig(cfg)}, nil
}

// ListNATGateways lists available NAT Gateways via EC2 DescribeNatGateways.
func (p *SDKProvider) ListNATGateways(ctx context.Context) ([]audit.Gateway, error) {
	var out []audit.Gateway
	pager := ec2.NewDescribeNatGatewaysPaginator(p.ec2, &ec2.DescribeNatGatewaysInput{
		Filter: []ec2types.Filter{{Name: aws.String("state"), Values: []string{"available", "pending"}}},
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, ng := range page.NatGateways {
			g := audit.Gateway{Region: p.region, State: string(ng.State)}
			if ng.NatGatewayId != nil {
				g.ID = *ng.NatGatewayId
			}
			if ng.VpcId != nil {
				g.VPCID = *ng.VpcId
			}
			if ng.SubnetId != nil {
				g.SubnetID = *ng.SubnetId
			}
			out = append(out, g)
		}
	}
	return out, nil
}

// GatewayMetrics sums outbound bytes and takes peak active connections from
// CloudWatch for the given NAT Gateway.
func (p *SDKProvider) GatewayMetrics(ctx context.Context, natID string, start, end time.Time, periodSec int32) (audit.Metrics, error) {
	sumBytes := func(metric string) (float64, error) {
		res, err := p.cw.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
			Namespace:  aws.String("AWS/NATGateway"),
			MetricName: aws.String(metric),
			Dimensions: []cwtypes.Dimension{{Name: aws.String("NatGatewayId"), Value: aws.String(natID)}},
			StartTime:  aws.Time(start), EndTime: aws.Time(end),
			Period:     aws.Int32(periodSec),
			Statistics: []cwtypes.Statistic{cwtypes.StatisticSum},
		})
		if err != nil {
			return 0, err
		}
		var total float64
		for _, d := range res.Datapoints {
			if d.Sum != nil {
				total += *d.Sum
			}
		}
		return total, nil
	}
	outDest, err := sumBytes("BytesOutToDestination")
	if err != nil {
		return audit.Metrics{}, err
	}
	outSrc, err := sumBytes("BytesOutToSource")
	if err != nil {
		return audit.Metrics{}, err
	}

	conns, err := p.cw.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String("AWS/NATGateway"),
		MetricName: aws.String("ActiveConnectionCount"),
		Dimensions: []cwtypes.Dimension{{Name: aws.String("NatGatewayId"), Value: aws.String(natID)}},
		StartTime:  aws.Time(start), EndTime: aws.Time(end),
		Period:     aws.Int32(periodSec),
		Statistics: []cwtypes.Statistic{cwtypes.StatisticMaximum},
	})
	if err != nil {
		return audit.Metrics{}, err
	}
	var peak float64
	for _, d := range conns.Datapoints {
		if d.Maximum != nil && *d.Maximum > peak {
			peak = *d.Maximum
		}
	}

	return audit.Metrics{
		ObservedGB:            (outDest + outSrc) / (1024 * 1024 * 1024),
		PeakActiveConnections: peak,
	}, nil
}
