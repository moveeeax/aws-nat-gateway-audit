# aws-nat-gateway-audit

[![ci](https://github.com/moveeeax/aws-nat-gateway-audit/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/aws-nat-gateway-audit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/moveeeax/aws-nat-gateway-audit.svg)](https://pkg.go.dev/github.com/moveeeax/aws-nat-gateway-audit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/moveeeax/aws-nat-gateway-audit)](https://goreportcard.com/report/github.com/moveeeax/aws-nat-gateway-audit)

Find idle or over-provisioned AWS NAT Gateways and estimate the monthly waste they represent.

NAT Gateways are a quiet cost sink: **$0.045/hr just to exist**, plus **$0.045/GB processed**, per Availability Zone. Teams spin one up per subnet, forget them, and route S3/DynamoDB traffic through them when a free gateway VPC endpoint would do the job. Those charges hide inside the *Data Transfer* and *EC2-Other* line items, and no console view tells you which gateways are idle or what each one costs. `aws-nat-gateway-audit` gives you a scriptable answer.

## What it does

- Lists every available NAT Gateway in a region (or across several with `--all-regions` / `--regions`).
- Pulls CloudWatch `BytesOutToDestination` + `BytesOutToSource` and peak `ActiveConnectionCount` over a lookback window and classifies each gateway **IDLE** or **ACTIVE**.
- Estimates monthly cost = `hourly * 730 + processedGB * per-GB`, using a built-in per-region price table (override with `--price-hourly` / `--price-per-gb`).
- Emits `table`, `json`, or `csv`, and **exits non-zero when idle waste exceeds `--threshold`** so you can gate it in CI.

## Install

```sh
go install github.com/moveeeax/aws-nat-gateway-audit@latest
```

Or build from source:

```sh
git clone https://github.com/moveeeax/aws-nat-gateway-audit
cd aws-nat-gateway-audit
go build -o aws-nat-gateway-audit .
```

## Usage

Audit one region over a 14-day window and fail if idle waste tops $30/mo:

```sh
aws-nat-gateway-audit --region eu-central-1 --days 14 --threshold 30 --output table
```

Try it with no AWS credentials using the built-in demo data:

```console
$ aws-nat-gateway-audit --demo --region eu-central-1 --days 14
NAT_GATEWAY           REGION        VPC        VERDICT  GB/MO   $/MO
nat-0bb22busy0000002  eu-central-1  vpc-demo1  ACTIVE   1741.1  128.50
nat-0cc33s3only00003  eu-central-1  vpc-demo2  ACTIVE   514.3   64.70
nat-0aa11idle0000001  eu-central-1  vpc-demo1  IDLE     0.6     37.99
nat-0dd44dead000004   eu-central-1  vpc-demo2  IDLE     0.0     37.96

IDLE waste: $75.95/mo across 2 gateway(s)
```

JSON and CSV output are in [`examples/`](examples/).

### Flags

| Flag | Default | Description |
|---|---|---|
| `--region` | from AWS config/env | Region to audit |
| `--all-regions` | `false` | Audit a built-in list of common regions |
| `--regions` | — | Explicit comma-separated region list |
| `--days` | `14` | CloudWatch lookback window |
| `--output` | `table` | `table` \| `json` \| `csv` |
| `--threshold` | `0` | Exit non-zero if idle waste $/mo exceeds this (`0` = never fail) |
| `--idle-gb-floor` | `1.0` | GB/window below which a gateway counts as idle |
| `--idle-conn-floor` | `5` | Peak connections below which a gateway counts as idle |
| `--price-hourly` / `--price-per-gb` | table | Override the price model for all regions |
| `--demo` | `false` | Use built-in fake data (no AWS calls) |

### Use it in CI

```yaml
- name: Fail on idle NAT Gateway waste
  run: aws-nat-gateway-audit --all-regions --threshold 25
```

Exit codes: `0` clean, `2` threshold breached, `1` runtime error.

## The cost model

A gateway is **idle** only when it processed less than `--idle-gb-floor` GB **and** saw fewer than `--idle-conn-floor` peak connections over the window — requiring both avoids flagging a low-throughput but genuinely live gateway. Observed traffic is extrapolated to a 30-day month, then priced. The built-in table tracks public list prices for common regions and falls back to us-east-1 rates for anything unlisted; pass `--price-*` for exact numbers.

## AWS permissions

`ec2:DescribeNatGateways` and `cloudwatch:GetMetricStatistics`. Read-only.

## Roadmap

- Flag route tables that could use a gateway VPC endpoint (S3/DynamoDB) instead of the NAT path.
- Pull live per-region pricing from the AWS Pricing API.

## License

[MIT](LICENSE)
