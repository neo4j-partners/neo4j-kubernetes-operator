# Query Performance Monitoring Guide

This guide provides comprehensive instructions for implementing advanced query performance monitoring with the Neo4j Kubernetes Operator.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Basic Configuration](#basic-configuration)
- [Advanced Configuration](#advanced-configuration)
- [Query Profiling](#query-profiling)
- [Performance Metrics](#performance-metrics)
- [Alerting and Notifications](#alerting-and-notifications)
- [Query Optimization](#query-optimization)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)
- [Integration Examples](#integration-examples)

## Overview

Query performance monitoring enables real-time tracking, analysis, and optimization of Cypher queries in Neo4j clusters. This feature provides comprehensive insights into query execution patterns, resource utilization, and performance bottlenecks.

### Key Features

- **Real-time Query Tracking**: Monitor all queries as they execute
- **Performance Profiling**: Deep analysis of query execution plans
- **Slow Query Detection**: Automatic identification of problematic queries
- **Resource Monitoring**: Track CPU, memory, and I/O usage per query
- **Alert Integration**: Proactive notifications for performance issues
- **Historical Analysis**: Long-term query performance trends

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Neo4j Cluster                            │
├─────────────────────────────────────────────────────────────┤
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐    │
│  │  Neo4j Node 1 │  │  Neo4j Node 2 │  │  Neo4j Node 3 │    │
│  │               │  │               │  │               │    │
│  │ Query Monitor │  │ Query Monitor │  │ Query Monitor │    │
│  └─────┬─────────┘  └─────┬─────────┘  └─────┬─────────┘    │
└────────┼─────────────────┼─────────────────┼────────────────┘
         │                 │                 │
         ▼                 ▼                 ▼
┌─────────────────────────────────────────────────────────────┐
│                Metrics Aggregator                           │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Prometheus  │  │   Grafana   │  │     AlertManager    │  │
│  │             │  │             │  │                     │  │
│  │   Metrics   │  │ Dashboards  │  │  Notifications      │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Prerequisites

### Infrastructure Requirements

- Neo4j Enterprise Edition 5.0+
- Prometheus for metrics collection
- Grafana for visualization
- AlertManager for notifications
- Sufficient storage for query logs and metrics

### Dependencies

```yaml
# Required monitoring stack
dependencies:
  - name: prometheus-operator
    version: ">=0.60.0"
  - name: grafana
    version: ">=9.0.0"
  - name: alertmanager
    version: ">=0.24.0"
```

### Permissions

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: neo4j-query-monitor
rules:
- apiGroups: [""]
  resources: ["pods", "services", "endpoints"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["monitoring.coreos.com"]
  resources: ["servicemonitors", "prometheusrules"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
```

## Basic Configuration

### Enable Query Monitoring

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-monitored
  namespace: neo4j-system
spec:
  # Query monitoring configuration
  queryMonitoring:
    enabled: true

    # Collection settings
    collection:
      samplingRate: 1.0  # Collect all queries (0.0-1.0)
      slowQueryThreshold: "1s"
      longRunningQueryThreshold: "30s"

      # Query categorization
      categories:
        - name: "read-queries"
          pattern: "MATCH.*RETURN"
        - name: "write-queries"
          pattern: "(CREATE|MERGE|SET|DELETE)"
        - name: "admin-queries"
          pattern: "(SHOW|CALL)"

    # Storage configuration
    storage:
      retention: "30d"
      maxSize: "10Gi"

    # Export settings
    export:
      prometheus:
        enabled: true
        interval: "30s"
        endpoint: "prometheus-operated:9090"

      elasticsearch:
        enabled: false
        endpoint: "elasticsearch:9200"
        index: "neo4j-queries"

    # Performance profiling
    profiling:
      enabled: true
      detailedPlans: true
      includeRuntimeStatistics: true
      trackMemoryUsage: true
      trackIOOperations: true

  # Resource allocation for monitoring
  monitoring:
    resources:
      requests:
        memory: "512Mi"
        cpu: "100m"
      limits:
        memory: "1Gi"
        cpu: "500m"
```

### Prometheus ServiceMonitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: neo4j-query-metrics
  namespace: neo4j-system
spec:
  selector:
    matchLabels:
      app: neo4j-monitored
      component: metrics
  endpoints:
  - port: monitoring
    interval: 30s
    path: /metrics
    scrapeTimeout: 10s
  - port: bolt-metrics
    interval: 30s
    path: /bolt/metrics
    scrapeTimeout: 10s
```

## Advanced Configuration

### Comprehensive Monitoring Setup

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-advanced-monitoring
  namespace: neo4j-system
spec:
  queryMonitoring:
    enabled: true

    # Advanced collection settings
    collection:
      samplingRate: 0.1  # Sample 10% of queries for performance
      adaptiveSampling:
        enabled: true
        baseRate: 0.05
        slowQueryRate: 1.0  # Always collect slow queries
        errorRate: 1.0      # Always collect failed queries

      # Query filtering
      filters:
        - type: "exclude"
          pattern: "CALL db.ping()"  # Exclude health checks
        - type: "exclude"
          pattern: "SHOW DATABASES"
        - type: "include"
          pattern: ".*"  # Include all other queries

      # Detailed tracking
      trackParameters: true
      trackUserInfo: true
      trackConnectionInfo: true
      trackQuerySource: true

    # Performance thresholds
    thresholds:
      slow:
        duration: "500ms"
        severity: "warning"
      critical:
        duration: "5s"
        severity: "critical"
      memory:
        peak: "1Gi"
        severity: "warning"

    # Query analysis
    analysis:
      enabled: true
      patternDetection: true
      anomalyDetection:
        enabled: true
        baselineWindow: "7d"
        sensitivityLevel: "medium"

      # Query recommendations
      recommendations:
        enabled: true
        indexSuggestions: true
        queryRewriting: true
        planOptimizations: true

    # Advanced profiling
    profiling:
      enabled: true
      modes:
        - "PROFILE"
        - "EXPLAIN"

      # Execution plan analysis
      planAnalysis:
        enabled: true
        trackOperators: true
        trackCardinalities: true
        trackCacheHits: true

      # Resource tracking
      resourceTracking:
        trackCPU: true
        trackMemory: true
        trackIO: true
        trackNetworking: true

    # Multi-dimensional metrics
    metrics:
      dimensions:
        - "user"
        - "database"
        - "query_type"
        - "execution_mode"
        - "query_source"

      # Custom metrics
      custom:
        - name: "business_queries"
          description: "Business logic queries"
          query: ".*business.*"
        - name: "analytical_queries"
          description: "Analytical workload queries"
          query: ".*(aggregat|group|order).*"

    # Real-time streaming
    streaming:
      enabled: true
      kafka:
        enabled: true
        brokers: ["kafka:9092"]
        topic: "neo4j-query-events"

      websocket:
        enabled: true
        port: 8080
        maxConnections: 100
```

### Multi-Database Monitoring

```yaml
apiVersion: neo4j.neo4j.com/v1alpha1
kind: Neo4jEnterpriseCluster
metadata:
  name: neo4j-multi-db-monitoring
  namespace: neo4j-system
spec:
  queryMonitoring:
    enabled: true

    # Per-database configuration
    databases:
      - name: "production"
        samplingRate: 0.5
        slowQueryThreshold: "200ms"
        alerts:
          - type: "slow_query"
            threshold: "1s"
            severity: "warning"

      - name: "analytics"
        samplingRate: 0.1
        slowQueryThreshold: "5s"
        alerts:
          - type: "slow_query"
            threshold: "30s"
            severity: "warning"

      - name: "development"
        samplingRate: 1.0
        slowQueryThreshold: "100ms"
        alerts:
          - type: "slow_query"
            threshold: "500ms"
            severity: "info"

    # Cross-database analysis
    crossDatabaseAnalysis:
      enabled: true
      comparePerformance: true
      detectMigrationPatterns: true
      trackResourceUsage: true
```

## Query Profiling

### Execution Plan Analysis

```yaml
# Query profiling configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: neo4j-query-profiling
  namespace: neo4j-system
data:
  profiling.conf: |
    # Enable detailed execution plan tracking
    dbms.logs.query.enabled=true
    dbms.logs.query.threshold=100ms
    dbms.logs.query.parameter_logging_enabled=true
    dbms.logs.query.time_logging_enabled=true
    dbms.logs.query.allocation_logging_enabled=true
    dbms.logs.query.page_logging_enabled=true

    # Runtime statistics
    dbms.logs.query.runtime_logging_enabled=true
    dbms.logs.query.max_parameter_length=1000

    # Query log rotation
    dbms.logs.query.rotation.keep_number=10
    dbms.logs.query.rotation.size=100M
```

### Performance Profiling Scripts

```bash
#!/bin/bash
# scripts/profile-queries.sh

NAMESPACE=${1:-neo4j-system}
DATABASE=${2:-neo4j}
DURATION=${3:-600}  # 10 minutes

echo "Starting query profiling for database: $DATABASE"

NEO4J_POD=$(kubectl get pods -l app=neo4j-monitored -n $NAMESPACE -o jsonpath='{.items[0].metadata.name}')

# Enable query logging
kubectl exec $NEO4J_POD -n $NAMESPACE -- cypher-shell -u neo4j -p $NEO4J_PASSWORD -d $DATABASE "
CALL dbms.setConfigValue('dbms.logs.query.enabled', 'true');
CALL dbms.setConfigValue('dbms.logs.query.threshold', '0ms');
"

# Run profiling for specified duration
echo "Profiling queries for $DURATION seconds..."
sleep $DURATION

# Collect query logs
kubectl exec $NEO4J_POD -n $NAMESPACE -- find /logs -name "query.log*" -exec cat {} \; > query-profile-$(date +%Y%m%d-%H%M%S).log

# Analyze slow queries
kubectl exec $NEO4J_POD -n $NAMESPACE -- cypher-shell -u neo4j -p $NEO4J_PASSWORD -d $DATABASE "
CALL db.stats.retrieve('QUERY') YIELD section, data
WHERE section = 'slow_queries'
RETURN data
"

echo "Query profiling completed. Results saved to query-profile-*.log"
```

### Query Plan Visualization

```python
#!/usr/bin/env python3
# scripts/visualize-query-plans.py

import json
import matplotlib.pyplot as plt
import networkx as nx
from matplotlib.patches import Rectangle

def visualize_execution_plan(plan_json):
    """Visualize Neo4j execution plan as a tree diagram"""

    plan = json.loads(plan_json)
    G = nx.DiGraph()

    def add_operator_nodes(operator, parent=None):
        node_id = id(operator)
        operator_type = operator.get('operatorType', 'Unknown')
        rows = operator.get('rows', 0)
        estimated_rows = operator.get('estimatedRows', 0)
        db_hits = operator.get('dbHits', 0)

        # Add node with attributes
        G.add_node(node_id,
                   type=operator_type,
                   rows=rows,
                   estimated_rows=estimated_rows,
                   db_hits=db_hits)

        if parent:
            G.add_edge(parent, node_id)

        # Process children
        for child in operator.get('children', []):
            add_operator_nodes(child, node_id)

        return node_id

    # Build graph from execution plan
    root = plan['plan']['root']
    add_operator_nodes(root)

    # Create visualization
    plt.figure(figsize=(16, 12))
    pos = nx.spring_layout(G, k=3, iterations=50)

    # Draw nodes
    for node, data in G.nodes(data=True):
        x, y = pos[node]

        # Color based on operator type
        colors = {
            'NodeByLabelScan': 'lightblue',
            'Filter': 'lightgreen',
            'Expand': 'lightyellow',
            'ProduceResults': 'lightcoral',
            'AllNodesScan': 'lightgray'
        }
        color = colors.get(data['type'], 'white')

        # Draw operator box
        rect = Rectangle((x-0.1, y-0.05), 0.2, 0.1,
                        facecolor=color, edgecolor='black')
        plt.gca().add_patch(rect)

        # Add operator text
        plt.text(x, y, f"{data['type']}\n{data['rows']} rows\n{data['db_hits']} hits",
                ha='center', va='center', fontsize=8)

    # Draw edges
    nx.draw_networkx_edges(G, pos, alpha=0.6, arrows=True)

    plt.title("Neo4j Query Execution Plan")
    plt.axis('off')
    plt.tight_layout()
    plt.savefig('execution_plan.png', dpi=300, bbox_inches='tight')
    plt.show()

if __name__ == "__main__":
    import sys
    if len(sys.argv) != 2:
        print("Usage: python visualize-query-plans.py <plan.json>")
        sys.exit(1)

    with open(sys.argv[1], 'r') as f:
        plan_json = f.read()

    visualize_execution_plan(plan_json)
```

## Performance Metrics

### Core Metrics

```yaml
# Prometheus recording rules for Neo4j queries
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: neo4j-query-metrics
  namespace: neo4j-system
spec:
  groups:
  - name: neo4j.query.performance
    interval: 30s
    rules:
    # Query throughput metrics
    - record: neo4j:query_rate_5m
      expr: rate(neo4j_cypher_queries_total[5m])

    - record: neo4j:query_error_rate_5m
      expr: rate(neo4j_cypher_queries_total{status="error"}[5m]) / rate(neo4j_cypher_queries_total[5m])

    # Response time metrics
    - record: neo4j:query_duration_p50
      expr: histogram_quantile(0.50, rate(neo4j_cypher_query_duration_seconds_bucket[5m]))

    - record: neo4j:query_duration_p95
      expr: histogram_quantile(0.95, rate(neo4j_cypher_query_duration_seconds_bucket[5m]))

    - record: neo4j:query_duration_p99
      expr: histogram_quantile(0.99, rate(neo4j_cypher_query_duration_seconds_bucket[5m]))

    # Resource utilization
    - record: neo4j:query_memory_peak_p95
      expr: histogram_quantile(0.95, rate(neo4j_cypher_query_memory_peak_bytes_bucket[5m]))

    - record: neo4j:query_cpu_usage_rate
      expr: rate(neo4j_cypher_query_cpu_seconds_total[5m])

    # Query complexity metrics
    - record: neo4j:query_dbhits_rate
      expr: rate(neo4j_cypher_query_dbhits_total[5m])

    - record: neo4j:query_page_faults_rate
      expr: rate(neo4j_cypher_query_page_faults_total[5m])

    # Business metrics
    - record: neo4j:slow_queries_rate
      expr: rate(neo4j_cypher_queries_total{duration_bucket=~".*_slow"}[5m])

    - record: neo4j:long_running_queries_count
      expr: neo4j_cypher_queries_running{duration=">30s"}
```

### Custom Metrics Dashboard

```json
{
  "dashboard": {
    "title": "Neo4j Query Performance",
    "tags": ["neo4j", "performance", "queries"],
    "panels": [
      {
        "title": "Query Throughput",
        "type": "graph",
        "targets": [
          {
            "expr": "neo4j:query_rate_5m",
            "legendFormat": "Queries/sec"
          }
        ],
        "yAxes": [
          {"label": "Queries per second"}
        ]
      },
      {
        "title": "Response Time Percentiles",
        "type": "graph",
        "targets": [
          {
            "expr": "neo4j:query_duration_p50",
            "legendFormat": "P50"
          },
          {
            "expr": "neo4j:query_duration_p95",
            "legendFormat": "P95"
          },
          {
            "expr": "neo4j:query_duration_p99",
            "legendFormat": "P99"
          }
        ],
        "yAxes": [
          {"label": "Response time (seconds)", "logBase": 10}
        ]
      },
      {
        "title": "Error Rate",
        "type": "stat",
        "targets": [
          {
            "expr": "neo4j:query_error_rate_5m * 100",
            "legendFormat": "Error %"
          }
        ],
        "thresholds": [
          {"value": 1, "color": "yellow"},
          {"value": 5, "color": "red"}
        ]
      },
      {
        "title": "Query Types Distribution",
        "type": "piechart",
        "targets": [
          {
            "expr": "sum by(query_type) (neo4j:query_rate_5m)",
            "legendFormat": "{{query_type}}"
          }
        ]
      },
      {
        "title": "Top Slow Queries",
        "type": "table",
        "targets": [
          {
            "expr": "topk(10, avg_over_time(neo4j_cypher_query_duration_seconds[1h])) by (query_hash)",
            "format": "table"
          }
        ],
        "columns": [
          {"text": "Query Hash", "value": "query_hash"},
          {"text": "Avg Duration", "value": "Value", "unit": "s"}
        ]
      },
      {
        "title": "Memory Usage by Query",
        "type": "graph",
        "targets": [
          {
            "expr": "neo4j:query_memory_peak_p95",
            "legendFormat": "P95 Memory Usage"
          }
        ],
        "yAxes": [
          {"label": "Memory (bytes)", "unit": "bytes"}
        ]
      }
    ]
  }
}
```

## Alerting and Notifications

### Prometheus Alerts

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: neo4j-query-alerts
  namespace: neo4j-system
spec:
  groups:
  - name: neo4j.query.alerts
    rules:
    # High query error rate
    - alert: Neo4jHighQueryErrorRate
      expr: neo4j:query_error_rate_5m > 0.05
      for: 2m
      labels:
        severity: warning
        component: neo4j
        type: query
      annotations:
        summary: "High query error rate detected"
        description: "Query error rate is {{ $value | humanizePercentage }} for cluster {{ $labels.cluster }}"
        runbook_url: "https://docs.company.com/runbooks/neo4j-high-error-rate"

    # Slow query performance
    - alert: Neo4jSlowQueryPerformance
      expr: neo4j:query_duration_p95 > 5
      for: 5m
      labels:
        severity: warning
        component: neo4j
        type: performance
      annotations:
        summary: "Slow query performance detected"
        description: "P95 query response time is {{ $value }}s for cluster {{ $labels.cluster }}"
        runbook_url: "https://docs.company.com/runbooks/neo4j-slow-queries"

    # Long running queries
    - alert: Neo4jLongRunningQueries
      expr: neo4j:long_running_queries_count > 10
      for: 1m
      labels:
        severity: critical
        component: neo4j
        type: performance
      annotations:
        summary: "Multiple long-running queries detected"
        description: "{{ $value }} queries have been running for more than 30 seconds"
        runbook_url: "https://docs.company.com/runbooks/neo4j-long-running-queries"

    # High memory usage
    - alert: Neo4jHighQueryMemoryUsage
      expr: neo4j:query_memory_peak_p95 > 2*1024*1024*1024  # 2GB
      for: 5m
      labels:
        severity: warning
        component: neo4j
        type: resource
      annotations:
        summary: "High query memory usage detected"
        description: "P95 query memory usage is {{ $value | humanizeBytes }}"
        runbook_url: "https://docs.company.com/runbooks/neo4j-high-memory"

    # Query throughput drop
    - alert: Neo4jQueryThroughputDrop
      expr: (neo4j:query_rate_5m offset 1h) - neo4j:query_rate_5m > 100
      for: 5m
      labels:
        severity: warning
        component: neo4j
        type: performance
      annotations:
        summary: "Significant query throughput drop"
        description: "Query rate dropped by {{ $value }} queries/sec compared to 1 hour ago"
```

### AlertManager Configuration

```yaml
# alertmanager.yml
global:
  slack_api_url: 'https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK'

route:
  group_by: ['alertname', 'cluster']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'neo4j-alerts'
  routes:
  - match:
      severity: critical
    receiver: 'neo4j-critical'
  - match:
      component: neo4j
    receiver: 'neo4j-alerts'

receivers:
- name: 'neo4j-alerts'
  slack_configs:
  - channel: '#neo4j-alerts'
    title: 'Neo4j Query Alert - {{ .GroupLabels.alertname }}'
    text: |
      {{ range .Alerts }}
      *Alert:* {{ .Annotations.summary }}
      *Description:* {{ .Annotations.description }}
      *Cluster:* {{ .Labels.cluster }}
      *Severity:* {{ .Labels.severity }}
      *Runbook:* {{ .Annotations.runbook_url }}
      {{ end }}

- name: 'neo4j-critical'
  slack_configs:
  - channel: '#neo4j-critical'
    title: '🚨 CRITICAL Neo4j Alert - {{ .GroupLabels.alertname }}'
    text: |
      {{ range .Alerts }}
      *Alert:* {{ .Annotations.summary }}
      *Description:* {{ .Annotations.description }}
      *Cluster:* {{ .Labels.cluster }}
      *Runbook:* {{ .Annotations.runbook_url }}
      {{ end }}
  pagerduty_configs:
  - service_key: 'YOUR_PAGERDUTY_SERVICE_KEY'
    description: '{{ .GroupLabels.alertname }} - {{ .CommonAnnotations.summary }}'
```

## Query Optimization

### Automatic Index Recommendations

```python
#!/usr/bin/env python3
# scripts/index-recommendations.py

import re
from collections import defaultdict, Counter

class IndexRecommendationEngine:
    def __init__(self):
        self.query_patterns = []
        self.property_usage = defaultdict(list)
        self.label_usage = Counter()

    def analyze_query_log(self, log_file):
        """Analyze query log and extract patterns"""
        with open(log_file, 'r') as f:
            for line in f:
                if 'MATCH' in line:
                    self._extract_patterns(line)

    def _extract_patterns(self, query):
        """Extract indexable patterns from queries"""
        # Find label usage
        label_matches = re.findall(r':\s*(\w+)', query)
        for label in label_matches:
            self.label_usage[label] += 1

        # Find property usage in WHERE clauses
        where_matches = re.findall(r'WHERE.*?(\w+)\.(\w+)\s*[=<>]', query, re.IGNORECASE)
        for label_prop in where_matches:
            self.property_usage[label_prop[1]].append(label_prop[0])

    def generate_recommendations(self):
        """Generate index recommendations"""
        recommendations = []

        # Single property indexes
        for prop, labels in self.property_usage.items():
            if len(labels) > 5:  # Property used frequently
                most_common_label = Counter(labels).most_common(1)[0][0]
                recommendations.append({
                    'type': 'single_property',
                    'command': f'CREATE INDEX FOR (n:{most_common_label}) ON (n.{prop})',
                    'reason': f'Property {prop} used {len(labels)} times with label {most_common_label}',
                    'priority': 'high' if len(labels) > 20 else 'medium'
                })

        # Composite indexes (for multi-property WHERE clauses)
        composite_patterns = self._find_composite_patterns()
        for pattern in composite_patterns:
            recommendations.append({
                'type': 'composite',
                'command': pattern['command'],
                'reason': pattern['reason'],
                'priority': pattern['priority']
            })

        return recommendations

    def _find_composite_patterns(self):
        """Find patterns that would benefit from composite indexes"""
        # Implementation for composite index detection
        return []

# Usage example
if __name__ == "__main__":
    engine = IndexRecommendationEngine()
    engine.analyze_query_log('query.log')

    recommendations = engine.generate_recommendations()

    print("Index Recommendations:")
    print("=" * 50)
    for rec in recommendations:
        print(f"Priority: {rec['priority'].upper()}")
        print(f"Command: {rec['command']}")
        print(f"Reason: {rec['reason']}")
        print("-" * 30)
```

### Query Rewriting Suggestions

```python
#!/usr/bin/env python3
# scripts/query-optimizer.py

import re

class QueryOptimizer:
    def __init__(self):
        self.optimization_rules = [
            self._optimize_multiple_matches,
            self._optimize_cartesian_products,
            self._optimize_node_scans,
            self._optimize_filtering,
            self._optimize_aggregations
        ]

    def optimize_query(self, query):
        """Apply optimization rules to a query"""
        optimized = query
        suggestions = []

        for rule in self.optimization_rules:
            result = rule(optimized)
            if result['optimized'] != optimized:
                suggestions.append(result['suggestion'])
                optimized = result['optimized']

        return {
            'original': query,
            'optimized': optimized,
            'suggestions': suggestions,
            'improvement_potential': self._estimate_improvement(query, optimized)
        }

    def _optimize_multiple_matches(self, query):
        """Combine multiple MATCH clauses where possible"""
        # Look for consecutive MATCH statements
        pattern = r'MATCH\s+([^;]+?)\s+MATCH\s+([^;]+?)(?=\s+WHERE|\s+RETURN|\s+WITH|$)'

        def combine_matches(match):
            return f"MATCH {match.group(1)}, {match.group(2)}"

        optimized = re.sub(pattern, combine_matches, query, flags=re.IGNORECASE | re.DOTALL)

        return {
            'optimized': optimized,
            'suggestion': 'Combined multiple MATCH clauses for better performance' if optimized != query else None
        }

    def _optimize_cartesian_products(self, query):
        """Detect and suggest fixes for cartesian products"""
        # Detect patterns that might create cartesian products
        if re.search(r'MATCH\s+\([^)]*\)\s*,\s*\([^)]*\)\s+WHERE\s+NOT', query, re.IGNORECASE):
            suggestion = "Potential cartesian product detected. Consider using relationship patterns instead of comma-separated nodes."
        else:
            suggestion = None

        return {
            'optimized': query,  # Don't auto-fix this one
            'suggestion': suggestion
        }

    def _optimize_node_scans(self, query):
        """Optimize full node scans"""
        # Replace MATCH (n) with more specific patterns where possible
        if re.search(r'MATCH\s+\([^:)]*\)\s+WHERE', query, re.IGNORECASE):
            return {
                'optimized': query,
                'suggestion': "Consider adding node labels to MATCH patterns to avoid full node scans"
            }

        return {'optimized': query, 'suggestion': None}

    def _optimize_filtering(self, query):
        """Optimize WHERE clause positioning"""
        # Move simple filters closer to MATCH clauses
        return {'optimized': query, 'suggestion': None}

    def _optimize_aggregations(self, query):
        """Optimize aggregation patterns"""
        return {'optimized': query, 'suggestion': None}

    def _estimate_improvement(self, original, optimized):
        """Estimate potential performance improvement"""
        if original == optimized:
            return 0

        # Simple heuristic based on complexity reduction
        original_complexity = len(re.findall(r'MATCH|WHERE|RETURN|WITH', original, re.IGNORECASE))
        optimized_complexity = len(re.findall(r'MATCH|WHERE|RETURN|WITH', optimized, re.IGNORECASE))

        return max(0, (original_complexity - optimized_complexity) / original_complexity * 100)

# Usage example
if __name__ == "__main__":
    optimizer = QueryOptimizer()

    test_query = """
    MATCH (u:User)
    MATCH (p:Product)
    WHERE u.id = $userId AND p.category = 'electronics'
    RETURN u.name, p.name
    """

    result = optimizer.optimize_query(test_query)

    print("Original Query:")
    print(result['original'])
    print("\nOptimized Query:")
    print(result['optimized'])
    print("\nSuggestions:")
    for suggestion in result['suggestions']:
        if suggestion:
            print(f"- {suggestion}")
    print(f"\nEstimated Improvement: {result['improvement_potential']:.1f}%")
```

## Troubleshooting

### Common Performance Issues

#### High Query Latency

**Diagnosis**:

```bash
# Check for slow queries
kubectl exec neo4j-0 -n neo4j-system -- cypher-shell -u neo4j -p $NEO4J_PASSWORD "
CALL db.stats.retrieve('QUERY') YIELD section, data
WHERE section = 'slow_queries'
RETURN data
ORDER BY data.duration DESC
LIMIT 10"

# Check for long-running queries
kubectl exec neo4j-0 -n neo4j-system -- cypher-shell -u neo4j -p $NEO4J_PASSWORD "
CALL dbms.listQueries() YIELD queryId, query, elapsedTimeMillis
WHERE elapsedTimeMillis > 30000
RETURN queryId, query, elapsedTimeMillis
ORDER BY elapsedTimeMillis DESC"
```

**Solutions**:

```bash
# Kill long-running queries
kubectl exec neo4j-0 -n neo4j-system -- cypher-shell -u neo4j -p $NEO4J_PASSWORD "
CALL dbms.killQuery('query-id')"

# Add missing indexes
kubectl exec neo4j-0 -n neo4j-system -- cypher-shell -u neo4j -p $NEO4J_PASSWORD "
CREATE INDEX user_id_index FOR (u:User) ON (u.id)"
```

#### Memory Issues

**Diagnosis**:

```bash
# Check memory usage by queries
kubectl exec neo4j-0 -n neo4j-system -- cypher-shell -u neo4j -p $NEO4J_PASSWORD "
CALL db.stats.retrieve('MEMORY') YIELD section, data
RETURN section, data"

# Check JVM heap usage
kubectl exec neo4j-0 -n neo4j-system -- grep -E "(heap|memory)" /logs/debug.log | tail -20
```

**Solutions**:

```bash
# Increase JVM heap size
kubectl patch neo4jenterprisecluster neo4j-monitored -n neo4j-system --type='merge' -p='
{
  "spec": {
    "env": [
      {
        "name": "NEO4J_dbms_memory_heap_initial__size",
        "value": "4G"
      },
      {
        "name": "NEO4J_dbms_memory_heap_max__size",
        "value": "8G"
      }
    ]
  }
}'
```

### Debug Commands

```bash
# Get query monitoring status
kubectl get neo4jenterprisecluster neo4j-monitored -n neo4j-system -o jsonpath='{.status.queryMonitoring}'

# Check monitoring pod logs
kubectl logs -n neo4j-system -l app=neo4j-monitored,component=monitoring

# Verify Prometheus metrics
kubectl port-forward -n neo4j-system svc/neo4j-monitored-metrics 8080:8080
curl http://localhost:8080/metrics | grep neo4j_cypher

# Check query log files
kubectl exec neo4j-0 -n neo4j-system -- ls -la /logs/query.log*

# View recent query activities
kubectl exec neo4j-0 -n neo4j-system -- tail -100 /logs/query.log
```

## Best Practices

### Query Monitoring Strategy

1. **Sampling Strategy**
   - Use adaptive sampling for high-traffic systems
   - Always monitor slow and failed queries at 100%
   - Adjust sampling based on system load

2. **Metric Collection**
   - Focus on actionable metrics
   - Implement proper retention policies
   - Use appropriate aggregation levels

3. **Alert Configuration**
   - Set meaningful thresholds based on SLA requirements
   - Implement escalation policies
   - Include runbook links in alerts

### Performance Optimization

1. **Index Management**
   - Regularly review index usage statistics
   - Remove unused indexes
   - Implement composite indexes for multi-property queries

2. **Query Patterns**
   - Encourage specific node labels
   - Avoid cartesian products
   - Use parameters for better query plan caching

3. **Resource Management**
   - Monitor memory usage patterns
   - Implement query timeouts
   - Use connection pooling

## Integration Examples

### Grafana Dashboard Integration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: neo4j-query-dashboard
  namespace: neo4j-system
  labels:
    grafana_dashboard: "1"
data:
  neo4j-queries.json: |
    {
      "dashboard": {
        "title": "Neo4j Query Performance",
        "panels": [
          {
            "title": "Query Performance Overview",
            "type": "row"
          },
          {
            "title": "Queries per Second",
            "type": "graph",
            "targets": [
              {
                "expr": "rate(neo4j_cypher_queries_total[5m])",
                "legendFormat": "{{instance}} - {{database}}"
              }
            ]
          },
          {
            "title": "Query Response Time",
            "type": "heatmap",
            "targets": [
              {
                "expr": "rate(neo4j_cypher_query_duration_seconds_bucket[5m])",
                "format": "heatmap"
              }
            ]
          }
        ]
      }
    }
```

### Custom Metrics Exporter

```python
#!/usr/bin/env python3
# Custom metrics exporter for advanced query analytics

from prometheus_client import Counter, Histogram, Gauge, start_http_server
import time
import threading
from neo4j import GraphDatabase

class Neo4jQueryMetricsExporter:
    def __init__(self, neo4j_uri, username, password):
        self.driver = GraphDatabase.driver(neo4j_uri, auth=(username, password))

        # Prometheus metrics
        self.query_count = Counter('neo4j_custom_queries_total',
                                 'Total number of queries',
                                 ['database', 'query_type'])

        self.query_duration = Histogram('neo4j_custom_query_duration_seconds',
                                      'Query duration in seconds',
                                      ['database', 'query_type'])

        self.active_connections = Gauge('neo4j_custom_active_connections',
                                      'Active connections count')

    def collect_metrics(self):
        """Collect metrics from Neo4j"""
        with self.driver.session() as session:
            # Collect query statistics
            result = session.run("""
                CALL db.stats.retrieve('QUERY') YIELD section, data
                RETURN section, data
            """)

            for record in result:
                self._process_query_stats(record['section'], record['data'])

            # Collect connection statistics
            result = session.run("CALL dbms.listConnections()")
            connection_count = len(list(result))
            self.active_connections.set(connection_count)

    def _process_query_stats(self, section, data):
        """Process query statistics and update metrics"""
        if section == 'query_summary':
            for query_type, stats in data.items():
                self.query_count.labels(
                    database=stats.get('database', 'unknown'),
                    query_type=query_type
                )._value._value = stats.get('count', 0)

                if 'avg_duration' in stats:
                    self.query_duration.labels(
                        database=stats.get('database', 'unknown'),
                        query_type=query_type
                    ).observe(stats['avg_duration'])

    def start_collection(self, interval=30):
        """Start metrics collection in background"""
        def collect_loop():
            while True:
                try:
                    self.collect_metrics()
                except Exception as e:
                    print(f"Error collecting metrics: {e}")
                time.sleep(interval)

        thread = threading.Thread(target=collect_loop, daemon=True)
        thread.start()

if __name__ == "__main__":
    exporter = Neo4jQueryMetricsExporter(
        "bolt://neo4j-service:7687",
        "neo4j",
        "password"
    )

    # Start metrics collection
    exporter.start_collection()

    # Start Prometheus metrics server
    start_http_server(8000)

    # Keep running
    while True:
        time.sleep(1)
```

## Related Documentation

- [Auto-Scaling Guide](./auto-scaling-guide.md)

- [Disaster Recovery Guide](./disaster-recovery-guide.md)
- [Plugin Management Guide](./plugin-management-guide.md)
- [Backup and Restore Guide](../backup-restore-guide.md)

---

*For additional support and advanced configurations, please refer to the [Neo4j Operator Documentation](../README.md) or contact the platform engineering team.*
