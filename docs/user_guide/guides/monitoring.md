# Monitoring

This guide explains how to monitor your Neo4j Enterprise clusters.

## Prometheus Integration

The operator integrates with Prometheus to expose metrics about your Neo4j cluster. You can enable Prometheus integration by setting the `spec.monitoring.prometheus.enabled` field to `true`.

## Grafana Dashboards

The operator includes a pre-built Grafana dashboard for visualizing your Neo4j metrics.
