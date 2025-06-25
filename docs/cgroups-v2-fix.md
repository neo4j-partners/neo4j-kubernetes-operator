# Cgroups v2 Fix for Kind Cluster Creation

## Problem Analysis

The original issue was that Kind was looking for cgroups v1 detection patterns (`"Reached target .*Multi-User System.*|detected cgroup v1"`) but modern systems, including GitHub Actions runners, use cgroups v2. This mismatch was causing cluster creation failures.

## Root Cause

1. **Outdated Detection Logic**: Kind's internal detection was looking for cgroups v1 patterns
2. **Modern Systems Use cgroups v2**: GitHub Actions runners and modern Linux distributions use cgroups v2
3. **Configuration Mismatch**: The existing cgroups configuration wasn't optimized for v2

## Solution Implemented

### 1. Created cgroups v2 Specific Configuration

**File**: `hack/kind-config-cgroups-v2.yaml`
- Explicitly configured for cgroups v2
- Set `cgroup-driver: "systemd"` (required for v2)
- Added `cgroup-root: "/"` for v2 compatibility
- Removed v1-specific settings

### 2. Enhanced Detection Logic

**File**: `.github/workflows/ci.yml`
- Added cgroup version detection in runner preparation
- Automatically detects cgroups v1 vs v2
- Uses appropriate configuration based on detected version

```bash
# Detect cgroup version
if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
    echo "Detected cgroups v2"
    export CGROUP_VERSION="v2"
else
    echo "Detected cgroups v1"
    export CGROUP_VERSION="v1"
fi
```

### 3. Updated Configuration Priority

The CI workflow now prioritizes cgroups v2 configuration:

1. **Simple Configuration** - Ultra-minimal, works with both versions
2. **Cgroups v2 Configuration** - Specifically for v2 systems
3. **Robust Configuration** - General purpose
4. **Other configurations** - Fallback options

### 4. Enhanced Testing

**Files**: Multiple test scripts
- `scripts/test-cgroups-v2.sh` - Tests cgroups v2 specifically
- Updated CI test script to auto-detect and use appropriate config
- Added Makefile target: `make test-cgroups-v2`

## Key Differences: cgroups v1 vs v2

| Aspect | cgroups v1 | cgroups v2 |
|--------|------------|------------|
| Detection | `/sys/fs/cgroup/cpu` | `/sys/fs/cgroup/cgroup.controllers` |
| Driver | `cgroupfs` or `systemd` | `systemd` (recommended) |
| Hierarchy | Multiple hierarchies | Single unified hierarchy |
| Configuration | `cgroup-parent` | `cgroup-root` |

## Configuration Comparison

### cgroups v1 Configuration (Original)
```yaml
kubeletExtraArgs:
  cgroup-driver: "cgroupfs"
  cgroup-parent: "/kubelet"
```

### cgroups v2 Configuration (New)
```yaml
kubeletExtraArgs:
  cgroup-driver: "systemd"
  cgroup-root: "/"
  cgroup-parent: ""
```

## Testing Strategy

### Local Testing
```bash
# Test cgroups v2 specifically
make test-cgroups-v2

# Test CI configuration (auto-detects version)
make test-ci-cluster

# Test all configurations
make test-cluster-configs
```

### CI Testing
The CI workflow automatically:
1. Detects cgroup version
2. Uses appropriate configuration
3. Falls back to simpler configs if needed

## Expected Results

With these fixes:

1. **Proper cgroup detection** - No more "cgroup v1" detection failures
2. **Modern system compatibility** - Works with cgroups v2 systems
3. **Automatic adaptation** - Detects and uses appropriate configuration
4. **Better reliability** - Multiple fallback options

## Troubleshooting

If cgroups v2 issues persist:

1. **Check cgroup version**: `ls -la /sys/fs/cgroup/cgroup.controllers`
2. **Verify systemd driver**: Check if systemd is available
3. **Test locally**: Use `make test-cgroups-v2` to reproduce
4. **Check Docker cgroup driver**: `docker info | grep -i cgroup`

## Why This Fixes the Original Issue

The original error `"Reached target .*Multi-User System.*|detected cgroup v1"` was occurring because:

1. Kind was looking for cgroups v1 patterns in the system logs
2. Modern systems use cgroups v2, so these patterns don't exist
3. The detection logic was failing, causing cluster creation to fail

By creating a cgroups v2-specific configuration and updating the detection logic, we bypass this issue entirely and use the appropriate configuration for the actual cgroup version present on the system.
