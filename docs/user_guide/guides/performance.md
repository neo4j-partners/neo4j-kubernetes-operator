# Performance

This guide explains how to tune the performance of your Neo4j Enterprise clusters.

## Resource Allocation

You can configure the CPU and memory resources for your Neo4j pods using the `spec.resources` field in the `Neo4jEnterpriseCluster` resource.

## JVM Tuning

You can tune the JVM settings for your Neo4j pods using the `spec.jvm` field.

## Autoscaling

The operator supports autoscaling to automatically adjust the size of your cluster based on the workload.
