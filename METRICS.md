# Metrics

The pod-label-webhook exposes Prometheus metrics on the `/metrics` endpoint. This document describes the available metrics and how to use them.

## Available Metrics

### Request Metrics

| Metric Name                                  | Type      | Description                        | Labels                     |
| -------------------------------------------- | --------- | ---------------------------------- | -------------------------- |
| `pod_label_webhook_requests_total`           | Counter   | Total number of requests processed | `path`, `method`, `status` |
| `pod_label_webhook_request_duration_seconds` | Histogram | Request duration in seconds        | `path`, `method`           |
| `pod_label_webhook_errors_total`             | Counter   | Total number of errors encountered | `path`, `method`, `status` |

### Health Metrics

| Metric Name                          | Type  | Description                                             | Labels |
| ------------------------------------ | ----- | ------------------------------------------------------- | ------ |
| `pod_label_webhook_readiness_status` | Gauge | Current readiness status (1 for ready, 0 for not ready) | None   |
| `pod_label_webhook_liveness_status`  | Gauge | Current liveness status (1 for alive, 0 for not alive)  | None   |

## Labels

### Request Metrics Labels

- `path`: The request path (e.g., `/mutate`, `/healthz`, `/readyz`)
- `method`: The HTTP method (e.g., `GET`, `POST`)
- `status`: The HTTP status code (e.g., `200`, `400`, `500`)

## Metric Types

- **Counter**: Monotonically increasing counter that only goes up
- **Histogram**: Measures the distribution of values (e.g., request durations)
- **Gauge**: Single numerical value that can go up and down

## Scraping Configuration

The metrics endpoint is configured to work with standard Prometheus scraping. The service is annotated with:

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8443"
    prometheus.io/path: "/metrics"
```

## Prometheus Service Monitor

If you're using the Prometheus Operator, you can use this ServiceMonitor configuration:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: pod-label-webhook
  namespace: webhook-test
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      app: pod-label-webhook
  namespaceSelector:
    matchNames:
      - webhook-test
  endpoints:
    - port: metrics
      scheme: https
      tlsConfig:
        insecureSkipVerify: true
      interval: 30s
      scrapeTimeout: 10s
      path: /metrics
```

## Example PromQL Queries

### Request Rate

```promql
# Request rate over the last 5 minutes
rate(pod_label_webhook_requests_total[5m])

# Error rate over the last 5 minutes
rate(pod_label_webhook_errors_total[5m])
```

### Latency

```promql
# 95th percentile latency over the last hour
histogram_quantile(0.95, sum(rate(pod_label_webhook_request_duration_seconds_bucket[1h])) by (le))

# Average request duration
rate(pod_label_webhook_request_duration_seconds_sum[5m]) /
rate(pod_label_webhook_request_duration_seconds_count[5m])
```

### Health Status

```promql
# Current readiness status
pod_label_webhook_readiness_status

# Current liveness status
pod_label_webhook_liveness_status
```

## Example Alerts

```yaml
groups:
  - name: pod-label-webhook
    rules:
      - alert: HighErrorRate
        expr: |
          sum(rate(pod_label_webhook_errors_total[5m])) /
          sum(rate(pod_label_webhook_requests_total[5m])) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High error rate in pod-label-webhook
          description: Error rate is above 10% for the last 5 minutes

      - alert: HighLatency
        expr: |
          histogram_quantile(0.95,
            sum(rate(pod_label_webhook_request_duration_seconds_bucket[5m]))
            by (le)
          ) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High latency in pod-label-webhook
          description: 95th percentile latency is above 1 second for the last 5 minutes

      - alert: WebhookNotReady
        expr: pod_label_webhook_readiness_status == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: Pod label webhook is not ready
          description: Readiness probe has been failing for 5 minutes
```

## Example Grafana Dashboard

A Grafana dashboard JSON model is available in the `dashboards` directory that includes:

- Request rate and error rate panels
- Latency distribution graphs
- Health status indicators
- Top endpoints by request volume
- Error rate breakdown by status code

## Metric Retention and Storage

Consider the following when planning metric retention:

1. The `request_duration_seconds` histogram has default buckets which may need tuning based on your latency profile
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
