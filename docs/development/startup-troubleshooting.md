# Neo4j Operator Startup Troubleshooting Guide

## Understanding Operator Startup

The Neo4j Operator startup process involves several phases that can appear as if the operator is "stuck" or "hanging". This is **normal behavior**.

### Normal Startup Sequence

1. **Controller Registration** (0-5 seconds)
   - All 8 controllers register their event sources
   - Workers start for each controller
   - You'll see logs like: `Starting workers {"controller": "neo4jgrant", ...}`

2. **Informer Cache Sync** (30-60 seconds) ⏳
   - **This is where it appears to "hang"**
   - The operator connects to Kubernetes API
   - Loads and caches all relevant CRDs and resources
   - **No visible logs during this phase** - this is normal!

3. **Ready State** (after cache sync)
   - Health endpoint responds (`/healthz`)
   - Metrics endpoint responds (`/metrics`)
   - Operator ready to process resources

## Why Does Startup Take So Long?

### Normal Reasons (30-60 seconds)
- **First-time startup**: Initial cache population
- **Many CRDs**: Neo4j operator watches 8+ custom resources
- **Kubernetes API latency**: Network delays to cluster
- **Resource discovery**: Finding existing Neo4j resources

### Extended Delays (60+ seconds)
- **Slow cluster connection**: Network issues or overloaded API server
- **Large number of existing resources**: Many Neo4j clusters already deployed
- **Resource constraints**: Low CPU/memory on development machine
- **Cluster authentication issues**: Kubeconfig problems

## How to Monitor Startup

### 1. Use Enhanced Development Script
```bash
make dev-start
```
**Expected output:**
```
[INFO] ⏳ Initial startup may take 30-60 seconds while informer caches sync
[INFO] 💡 This is normal behavior - the operator is connecting to Kubernetes and loading CRDs

# ... controller startup logs ...

[SUCCESS] 🚀 Operator is ready and healthy!
```

### 2. Check Health Endpoint
```bash
# In another terminal
curl http://localhost:8083/healthz
```
- **Before ready**: Connection refused or timeout
- **After ready**: Returns `ok`

### 3. Check Metrics Endpoint
```bash
curl http://localhost:8082/metrics
```

### 4. Monitor Logs
```bash
# View real-time logs
tail -f logs/operator-*.log

# Or if running in cluster
kubectl logs -f deployment/neo4j-operator-controller-manager -n neo4j-operator-system
```

## Troubleshooting Common Issues

### ❌ Port Already in Use
```
ERROR setup unable to start manager {"error": "error listening on :8083: listen tcp :8083: bind: address already in use"}
```

**Solution:**
```bash
make dev-clean  # Enhanced cleanup with port checking
# or
make dev-stop   # Now includes process cleanup
```

### ❌ Startup Takes > 2 Minutes
**Possible causes:**
- Cluster connection issues
- Authentication problems
- Resource constraints

**Diagnosis:**
```bash
# Check cluster connectivity
kubectl cluster-info

# Check authentication
kubectl auth can-i get pods

# Check system resources
kubectl top nodes  # if available
```

### ❌ Controllers Don't Start
**Check for:**
- Missing CRDs: `kubectl get crd | grep neo4j`
- RBAC issues: Check service account permissions
- Webhook conflicts: Ensure `--enable-webhooks=false` for development

## Quick Reference

### Normal Startup Timeline
- **0-5s**: Controllers register
- **5-60s**: Cache sync (appears hung) ⏳
- **60s+**: Ready and responsive ✅

### Key Endpoints
- Health: `http://localhost:8083/healthz`
- Metrics: `http://localhost:8082/metrics`
- Logs: `logs/operator-YYYYMMDD-HHMMSS.log`

### Quick Commands
```bash
# Start with enhanced feedback
make dev-start

# Clean everything (processes + ports)
make dev-clean

# Check if ready
curl -f http://localhost:8083/healthz && echo "Ready!" || echo "Not ready"

# Force cleanup if stuck
make dev-clean
pkill -f "go run.*cmd/main.go"  # Nuclear option
```

## When to Be Concerned

**🟢 Normal (don't worry):**
- 30-60 seconds of apparent "hanging" after workers start
- No logs during cache sync phase
- High CPU during initial startup

**🟡 Investigate (after 2+ minutes):**
- No health endpoint response
- Continuous error logs
- Memory usage climbing continuously

**🔴 Problem (needs attention):**
- Immediate crashes or panics
- Port binding errors
- Authentication/authorization errors
- Out of memory errors

## Development Tips

1. **Use `dev-clean` liberally**: It's safe and thorough
2. **Monitor health endpoint**: Best way to know when ready
3. **Check logs**: `logs/` directory has timestamped files
4. **Be patient**: First startup is always slower
5. **Use `info` log level**: Less noise than `debug`

## Still Having Issues?

1. Check [Development Guide](developer-guide.md)
2. Review [Testing Guide](../testing-guide.md)
3. Inspect cluster state: `kubectl get all -A`
4. Check operator logs for specific errors
5. Try restarting development cluster: `make dev-cluster-delete && make dev-cluster`
