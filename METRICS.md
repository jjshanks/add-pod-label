# Metrics

The add-pod-label exposes Prometheus metrics on the `/metrics` endpoint. This document describes the available metrics and how to use them.

## Available Metrics

### Request Metrics

| Metric Name                              | Type      | Description                        | Labels                     |
| ---------------------------------------- | --------- | ---------------------------------- | -------------------------- |
| `add_pod_label_requests_total`           | Counter   | Total number of requests processed | `path`, `method`, `status` |
| `add_pod_label_request_duration_seconds` | Histogram | Request duration in seconds        | `path`, `method`           |
| `add_pod_label_errors_total`             | Counter   | Total number of errors encountered | `path`, `method`, `status` |

### Health Metrics

| Metric Name                      | Type  | Description                                             | Labels |
| -------------------------------- | ----- | ------------------------------------------------------- | ------ |
| `add_pod_label_readiness_status` | Gauge | Current readiness status (1 for ready, 0 for not ready) | None   |
| `add_pod_label_liveness_status`  | Gauge | Current liveness status (1 for alive, 0 for not alive)  | None   |

## Labels

### Request Metrics Labels

- `path`: The request path (e.g., `/mutate`, `/healthz`, `/readyz`)
- `method`: The HTTP method (e.g., `GET`, `POST`)
- `status`: The HTTP status code (e.g., `200`, `400`, `500`)

## Metric Types

- **Counter**: Monotonically increasing counter that only goes up
- **Histogram**: Measures the distribution of values (e.g., request durations)
- **Gauge**: Single numerical value that can go up and down

## Histogram Buckets

The webhook uses custom histogram buckets optimized for typical webhook latencies (5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s). For more information on tuning histogram buckets, see the [Prometheus histogram documentation](https://prometheus.io/docs/practices/histograms/).

## Scraping Configuration

The metrics endpoint is configured to work with standard Prometheus scraping. The service is annotated with:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8443"
    prometheus.io/path: "/metrics"
```

## TLS Configuration

To properly configure TLS for metrics scraping:

1. Create a certificate for metrics scraping:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: webhook-metrics-cert
  namespace: webhook-test
spec:
  secretName: webhook-metrics-cert
  duration: 8760h # 1 year
  renewBefore: 720h # 30 days
  subject:
    organizations:
      - webhook-system
  commonName: add-pod-label-metrics
  dnsNames:
    - add-pod-label.webhook-test.svc
    - add-pod-label.webhook-test.svc.cluster.local
  issuerRef:
    name: webhook-selfsigned-issuer
    kind: Issuer
```

2. Configure the ServiceMonitor to use the certificate:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: add-pod-label
  namespace: webhook-test
spec:
  selector:
    matchLabels:
      app: add-pod-label
  namespaceSelector:
    matchNames:
      - webhook-test
  endpoints:
    - port: metrics
      scheme: https
      tlsConfig:
        ca:
          secret:
            name: webhook-metrics-cert
            key: ca.crt
        cert:
          secret:
            name: webhook-metrics-cert
            key: tls.crt
        keySecret:
          name: webhook-metrics-cert
          key: tls.key
      interval: 30s
      scrapeTimeout: 10s
      path: /metrics
```

## Example PromQL Queries

### Request Rate

```promql
# Request rate over the last 5 minutes
rate(add_pod_label_requests_total[5m])

# Error rate over the last 5 minutes
rate(add_pod_label_errors_total[5m])
```

### Latency

```promql
# 95th percentile latency over the last hour
histogram_quantile(0.95, sum(rate(add_pod_label_request_duration_seconds_bucket[1h])) by (le))

# Average request duration
rate(add_pod_label_request_duration_seconds_sum[5m]) /
rate(add_pod_label_request_duration_seconds_count[5m])
```

### Health Status

```promql
# Current readiness status
add_pod_label_readiness_status

# Current liveness status
add_pod_label_liveness_status
```

## Example Alerts

```yaml
groups:
  - name: add-pod-label
    rules:
      - alert: HighErrorRate
        expr: |
          sum(rate(add_pod_label_errors_total[5m])) /
          sum(rate(add_pod_label_requests_total[5m])) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High error rate in add-pod-label
          description: Error rate is above 10% for the last 5 minutes

      - alert: HighLatency
        expr: |
          histogram_quantile(0.95,
            sum(rate(add_pod_label_request_duration_seconds_bucket[5m]))
            by (le)
          ) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High latency in add-pod-label
          description: 95th percentile latency is above 1 second for the last 5 minutes

      - alert: WebhookNotReady
        expr: add_pod_label_readiness_status == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: Pod label webhook is not ready
          description: Readiness probe has been failing for 5 minutes
```

## Example Grafana Dashboard

The Grafana dashboard JSON can be found in [dashboards/add-pod-label.json](dashboards/add-pod-label.json).

1. Request Rate gauge
2. Request Duration (P95) time series
3. Readiness Status indicator
4. Error Rate by Path time series

## Metric Retention and Storage

Consider the following when planning metric retention:

1. The `request_duration_seconds` histogram has custom buckets optimized for webhook latencies
2. Counter metrics are relatively low cardinality and safe for long-term storage
3. Health metrics are point-in-time and can be downsampled aggressively

## Best Practices

1. Monitor both the success rate and latency of mutation requests
2. Set up alerts for error spikes and latency increases
3. Track the correlation between health status and error rates
4. Consider adding custom dashboards for your specific use cases
5. Use recording rules for frequently-used queries

## Development

When developing new features, consider:

1. Adding relevant metrics for new functionality
2. Following the existing naming scheme
3. Adding appropriate labels for better filtering
4. Documenting new metrics in this guide
5. Including example queries for new metrics

## Testing

The metrics implementation includes extensive testing:

1. Unit tests for metric registration and updates
2. Integration tests for metric collection
3. Tests for metric endpoint output
4. Label validation tests

You can run the metrics tests specifically with:

```bash
go test -v ./... -run TestMetrics
```