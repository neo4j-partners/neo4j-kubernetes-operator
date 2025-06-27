# Monitoring

This guide explains how to monitor your Neo4j Enterprise clusters.

## Prometheus Integration

The operator integrates with Prometheus to expose metrics about your Neo4j cluster. You can enable Prometheus integration by setting the `spec.monitoring.prometheus.enabled` field to `true`.

The operator will then expose a metrics endpoint that can be scraped by Prometheus. This allows you to monitor key metrics like transaction rates, memory usage, and query performance.

## Grafana Dashboards

The operator includes a pre-built Grafana dashboard for visualizing your Neo4j metrics. You can import this dashboard into your Grafana instance to get a comprehensive overview of your cluster's health and performance.

## Health Checks

The operator performs regular health checks on your Neo4j cluster to ensure that it is running correctly. You can view the health of your cluster by checking the `status` of the `Neo4jEnterpriseCluster` resource.
