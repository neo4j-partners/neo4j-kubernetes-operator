#!/bin/bash
# validate-topology.sh - Comprehensive Neo4j cluster topology validation
# Usage: ./validate-topology.sh <cluster-name> [namespace] [--format=table|json|yaml]

set -euo pipefail

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly NC='\033[0m' # No Color

# Configuration
CLUSTER_NAME="${1:-}"
NAMESPACE="${2:-default}"
OUTPUT_FORMAT="table"
VERBOSE=false
CHECK_HEALTH=true
TIMEOUT=60

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_debug() {
    if [[ "${VERBOSE}" == "true" ]]; then
        echo -e "${BLUE}[DEBUG]${NC} $1" >&2
    fi
}

# Usage function
usage() {
    cat << EOF
Usage: $0 <cluster-name> [namespace] [options]

Arguments:
  cluster-name    Name of the Neo4j cluster to validate
  namespace       Kubernetes namespace (default: default)

Options:
  --format=FORMAT Output format: table, json, yaml (default: table)
  --verbose       Enable verbose output
  --no-health     Skip health checks
  --timeout=N     Timeout for health checks in seconds (default: 60)
  --help, -h      Show this help

Examples:
  $0 my-neo4j-cluster
  $0 my-neo4j-cluster production --format=json
  $0 my-neo4j-cluster default --verbose --timeout=120
EOF
    exit 1
}

# Validate prerequisites
validate_prerequisites() {
    if [[ -z "${CLUSTER_NAME}" ]]; then
        log_error "Cluster name is required"
        usage
    fi

    if ! command -v kubectl >/dev/null 2>&1; then
        log_error "kubectl is required but not installed"
        exit 1
    fi

    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi

    if ! kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
        log_error "Namespace '${NAMESPACE}' does not exist"
        exit 1
    fi

    log_debug "Prerequisites validation passed"
}

# Get cluster information
get_cluster_info() {
    log_debug "Retrieving cluster information for ${CLUSTER_NAME} in namespace ${NAMESPACE}"

    # Check if cluster exists
    if ! kubectl get neo4jenterprisecluster "${CLUSTER_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
        log_error "Neo4j cluster '${CLUSTER_NAME}' not found in namespace '${NAMESPACE}'"
        exit 1
    fi

    # Get cluster specification
    CLUSTER_SPEC=$(kubectl get neo4jenterprisecluster "${CLUSTER_NAME}" -n "${NAMESPACE}" -o json)

    # Extract key information
    PRIMARY_COUNT=$(echo "${CLUSTER_SPEC}" | jq -r '.spec.primaryCount // 3')
    SECONDARY_COUNT=$(echo "${CLUSTER_SPEC}" | jq -r '.spec.secondaryCount // 0')
    TOTAL_EXPECTED=$((PRIMARY_COUNT + SECONDARY_COUNT))

    # Check for topology configuration
    TOPOLOGY_ENABLED=$(echo "${CLUSTER_SPEC}" | jq -r '.spec.topology != null')

    if [[ "${TOPOLOGY_ENABLED}" == "true" ]]; then
        TOPOLOGY_MODE=$(echo "${CLUSTER_SPEC}" | jq -r '.spec.topology.placementConfig.mode // "prefer"')
        ZONES_REQUIRED=$(echo "${CLUSTER_SPEC}" | jq -r '.spec.topology.placementConfig.minimumZones // null')
    else
        TOPOLOGY_MODE="none"
        ZONES_REQUIRED="null"
    fi

    log_debug "Cluster info: Primary=${PRIMARY_COUNT}, Secondary=${SECONDARY_COUNT}, Topology=${TOPOLOGY_MODE}"
}

# Get pod distribution
get_pod_distribution() {
    log_debug "Analyzing pod distribution"

    # Get all pods for the cluster
    local pod_data
    pod_data=$(kubectl get pods -n "${NAMESPACE}" \
        -l "app.kubernetes.io/name=neo4j,app.kubernetes.io/instance=${CLUSTER_NAME}" \
        -o json 2>/dev/null || echo '{"items":[]}')

    # Initialize arrays
    declare -A zone_distribution
    declare -A node_distribution
    declare -A pod_roles
    declare -A pod_status

    RUNNING_PODS=0
    TOTAL_PODS=0

    # Process each pod
    while IFS= read -r pod_info; do
        if [[ -z "${pod_info}" ]]; then
            continue
        fi

        local pod_name node_name zone role status
        pod_name=$(echo "${pod_info}" | jq -r '.metadata.name')
        node_name=$(echo "${pod_info}" | jq -r '.spec.nodeName // "unscheduled"')
        status=$(echo "${pod_info}" | jq -r '.status.phase // "Unknown"')

        # Determine role from pod name
        if [[ "${pod_name}" =~ -primary- ]]; then
            role="primary"
        elif [[ "${pod_name}" =~ -secondary- ]]; then
            role="secondary"
        else
            role="unknown"
        fi

        pod_roles["${pod_name}"]="${role}"
        pod_status["${pod_name}"]="${status}"

        TOTAL_PODS=$((TOTAL_PODS + 1))
        if [[ "${status}" == "Running" ]]; then
            RUNNING_PODS=$((RUNNING_PODS + 1))
        fi

        # Get zone information
        if [[ "${node_name}" != "unscheduled" ]]; then
            zone=$(kubectl get node "${node_name}" \
                -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}' 2>/dev/null || echo "unknown")

            zone_distribution["${zone}"]=$((${zone_distribution["${zone}"]:-0} + 1))
            node_distribution["${node_name}"]=$((${node_distribution["${node_name}"]:-0} + 1))
        else
            zone="unscheduled"
            zone_distribution["${zone}"]=$((${zone_distribution["${zone}"]:-0} + 1))
        fi

        # Store for output
        POD_DISTRIBUTION+=("${pod_name}|${node_name}|${zone}|${role}|${status}")

    done < <(echo "${pod_data}" | jq -c '.items[]?' 2>/dev/null || true)

    # Store zone counts for analysis
    ZONE_COUNT=${#zone_distribution[@]}
    ZONES=($(printf '%s\n' "${!zone_distribution[@]}" | sort))

    log_debug "Found ${TOTAL_PODS} pods (${RUNNING_PODS} running) across ${ZONE_COUNT} zones"
}

# Analyze topology distribution
analyze_topology() {
    log_debug "Analyzing topology distribution"

    # Count pods by role and zone
    declare -A primary_zones
    declare -A secondary_zones

    for pod_info in "${POD_DISTRIBUTION[@]}"; do
        IFS='|' read -r pod_name node_name zone role status <<< "${pod_info}"

        if [[ "${role}" == "primary" && "${status}" == "Running" ]]; then
            primary_zones["${zone}"]=$((${primary_zones["${zone}"]:-0} + 1))
        elif [[ "${role}" == "secondary" && "${status}" == "Running" ]]; then
            secondary_zones["${zone}"]=$((${secondary_zones["${zone}"]:-0} + 1))
        fi
    done

    # Analyze distribution quality
    DISTRIBUTION_SCORE=100
    DISTRIBUTION_ISSUES=()

    # Check if all pods are scheduled
    if [[ ${zone_distribution["unscheduled"]:-0} -gt 0 ]]; then
        DISTRIBUTION_ISSUES+=("${zone_distribution["unscheduled"]} pods are unscheduled")
        DISTRIBUTION_SCORE=$((DISTRIBUTION_SCORE - 30))
    fi

    # Check zone distribution for primaries
    if [[ ${#primary_zones[@]} -lt 2 && ${PRIMARY_COUNT} -gt 1 ]]; then
        DISTRIBUTION_ISSUES+=("Primary pods are not distributed across multiple zones")
        DISTRIBUTION_SCORE=$((DISTRIBUTION_SCORE - 25))
    fi

    # Check for zone balance
    local max_pods_per_zone=0
    local min_pods_per_zone=999
    for zone in "${ZONES[@]}"; do
        if [[ "${zone}" != "unscheduled" ]]; then
            local count=${zone_distribution["${zone}"]}
            if [[ ${count} -gt ${max_pods_per_zone} ]]; then
                max_pods_per_zone=${count}
            fi
            if [[ ${count} -lt ${min_pods_per_zone} ]]; then
                min_pods_per_zone=${count}
            fi
        fi
    done

    if [[ ${max_pods_per_zone} -gt $((min_pods_per_zone + 1)) ]]; then
        DISTRIBUTION_ISSUES+=("Uneven pod distribution across zones (${min_pods_per_zone}-${max_pods_per_zone})")
        DISTRIBUTION_SCORE=$((DISTRIBUTION_SCORE - 15))
    fi

    # Check topology constraints
    if [[ "${TOPOLOGY_MODE}" == "enforce" && "${ZONES_REQUIRED}" != "null" ]]; then
        if [[ ${ZONE_COUNT} -lt ${ZONES_REQUIRED} ]]; then
            DISTRIBUTION_ISSUES+=("Only ${ZONE_COUNT} zones available, but ${ZONES_REQUIRED} required")
            DISTRIBUTION_SCORE=$((DISTRIBUTION_SCORE - 40))
        fi
    fi

    # Determine overall status
    if [[ ${DISTRIBUTION_SCORE} -ge 80 ]]; then
        DISTRIBUTION_STATUS="Excellent"
    elif [[ ${DISTRIBUTION_SCORE} -ge 60 ]]; then
        DISTRIBUTION_STATUS="Good"
    elif [[ ${DISTRIBUTION_SCORE} -ge 40 ]]; then
        DISTRIBUTION_STATUS="Fair"
    else
        DISTRIBUTION_STATUS="Poor"
    fi

    log_debug "Distribution analysis complete: ${DISTRIBUTION_STATUS} (${DISTRIBUTION_SCORE}%)"
}

# Check cluster health
check_cluster_health() {
    if [[ "${CHECK_HEALTH}" != "true" ]]; then
        HEALTH_STATUS="Skipped"
        return 0
    fi

    log_debug "Checking cluster health"

    local healthy_pods=0
    local timeout_remaining=${TIMEOUT}

    # Wait for pods to be ready
    while [[ ${timeout_remaining} -gt 0 ]]; do
        healthy_pods=0

        for pod_info in "${POD_DISTRIBUTION[@]}"; do
            IFS='|' read -r pod_name node_name zone role status <<< "${pod_info}"

            if [[ "${status}" == "Running" ]]; then
                # Check if pod is ready
                if kubectl get pod "${pod_name}" -n "${NAMESPACE}" \
                    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null | grep -q "True"; then
                    healthy_pods=$((healthy_pods + 1))
                fi
            fi
        done

        if [[ ${healthy_pods} -eq ${TOTAL_EXPECTED} ]]; then
            break
        fi

        sleep 5
        timeout_remaining=$((timeout_remaining - 5))
    done

    # Determine health status
    if [[ ${healthy_pods} -eq ${TOTAL_EXPECTED} ]]; then
        HEALTH_STATUS="Healthy"
    elif [[ ${healthy_pods} -gt $((TOTAL_EXPECTED / 2)) ]]; then
        HEALTH_STATUS="Degraded"
    else
        HEALTH_STATUS="Unhealthy"
    fi

    HEALTHY_PODS=${healthy_pods}
    log_debug "Health check complete: ${HEALTH_STATUS} (${healthy_pods}/${TOTAL_EXPECTED})"
}

# Output results in table format
output_table() {
    echo
    echo "=== Neo4j Cluster Topology Validation ==="
    echo "Cluster: ${CLUSTER_NAME}"
    echo "Namespace: ${NAMESPACE}"
    echo "Topology Mode: ${TOPOLOGY_MODE}"
    echo

    echo "=== Cluster Status ==="
    printf "%-20s %s\n" "Expected Pods:" "${TOTAL_EXPECTED} (${PRIMARY_COUNT} primary, ${SECONDARY_COUNT} secondary)"
    printf "%-20s %s\n" "Running Pods:" "${RUNNING_PODS}/${TOTAL_PODS}"
    printf "%-20s %s\n" "Health Status:" "${HEALTH_STATUS}"
    if [[ "${CHECK_HEALTH}" == "true" ]]; then
        printf "%-20s %s\n" "Ready Pods:" "${HEALTHY_PODS:-0}/${TOTAL_EXPECTED}"
    fi
    echo

    echo "=== Distribution Analysis ==="
    printf "%-20s %s\n" "Status:" "${DISTRIBUTION_STATUS}"
    printf "%-20s %s%%\n" "Score:" "${DISTRIBUTION_SCORE}"
    printf "%-20s %s\n" "Zones Used:" "${ZONE_COUNT}"
    echo

    if [[ ${#DISTRIBUTION_ISSUES[@]} -gt 0 ]]; then
        echo "=== Issues Found ==="
        for issue in "${DISTRIBUTION_ISSUES[@]}"; do
            echo "  ⚠️  ${issue}"
        done
        echo
    fi

    echo "=== Pod Distribution ==="
    printf "%-30s %-20s %-15s %-10s %-10s\n" "POD NAME" "NODE" "ZONE" "ROLE" "STATUS"
    printf "%-30s %-20s %-15s %-10s %-10s\n" "----------" "----" "----" "----" "------"

    for pod_info in "${POD_DISTRIBUTION[@]}"; do
        IFS='|' read -r pod_name node_name zone role status <<< "${pod_info}"
        printf "%-30s %-20s %-15s %-10s %-10s\n" "${pod_name}" "${node_name}" "${zone}" "${role}" "${status}"
    done
    echo

    echo "=== Zone Summary ==="
    printf "%-15s %-10s\n" "ZONE" "POD COUNT"
    printf "%-15s %-10s\n" "----" "---------"
    for zone in "${ZONES[@]}"; do
        printf "%-15s %-10s\n" "${zone}" "${zone_distribution["${zone}"]}"
    done
}

# Output results in JSON format
output_json() {
    local issues_json=""
    if [[ ${#DISTRIBUTION_ISSUES[@]} -gt 0 ]]; then
        issues_json=$(printf '%s\n' "${DISTRIBUTION_ISSUES[@]}" | jq -R . | jq -s .)
    else
        issues_json="[]"
    fi

    local pods_json="[]"
    if [[ ${#POD_DISTRIBUTION[@]} -gt 0 ]]; then
        pods_json="["
        local first=true
        for pod_info in "${POD_DISTRIBUTION[@]}"; do
            IFS='|' read -r pod_name node_name zone role status <<< "${pod_info}"
            if [[ "${first}" != "true" ]]; then
                pods_json+=","
            fi
            pods_json+="{\"name\":\"${pod_name}\",\"node\":\"${node_name}\",\"zone\":\"${zone}\",\"role\":\"${role}\",\"status\":\"${status}\"}"
            first=false
        done
        pods_json+="]"
    fi

    local zones_json="{"
    local first=true
    for zone in "${ZONES[@]}"; do
        if [[ "${first}" != "true" ]]; then
            zones_json+=","
        fi
        zones_json+="\"${zone}\":${zone_distribution["${zone}"]}"
        first=false
    done
    zones_json+="}"

    cat << EOF
{
  "cluster": "${CLUSTER_NAME}",
  "namespace": "${NAMESPACE}",
  "topology": {
    "mode": "${TOPOLOGY_MODE}",
    "zonesRequired": ${ZONES_REQUIRED:-null}
  },
  "status": {
    "expectedPods": ${TOTAL_EXPECTED},
    "runningPods": ${RUNNING_PODS},
    "totalPods": ${TOTAL_PODS},
    "healthStatus": "${HEALTH_STATUS}",
    "readyPods": ${HEALTHY_PODS:-0}
  },
  "distribution": {
    "status": "${DISTRIBUTION_STATUS}",
    "score": ${DISTRIBUTION_SCORE},
    "zonesUsed": ${ZONE_COUNT},
    "issues": ${issues_json}
  },
  "pods": ${pods_json},
  "zones": ${zones_json}
}
EOF
}

# Main function
main() {
    log_info "Starting Neo4j cluster topology validation"

    validate_prerequisites

    # Initialize arrays
    POD_DISTRIBUTION=()
    declare -A zone_distribution

    get_cluster_info
    get_pod_distribution
    analyze_topology
    check_cluster_health

    # Output results
    case "${OUTPUT_FORMAT}" in
        "table")
            output_table
            ;;
        "json")
            output_json
            ;;
        "yaml")
            output_json | yq eval -P
            ;;
        *)
            log_error "Unsupported output format: ${OUTPUT_FORMAT}"
            exit 1
            ;;
    esac

    # Exit with appropriate code
    if [[ "${HEALTH_STATUS}" == "Healthy" && ${DISTRIBUTION_SCORE} -ge 60 ]]; then
        log_success "Validation completed successfully"
        exit 0
    elif [[ "${HEALTH_STATUS}" == "Degraded" || ${DISTRIBUTION_SCORE} -ge 40 ]]; then
        log_warning "Validation completed with warnings"
        exit 1
    else
        log_error "Validation failed"
        exit 2
    fi
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --format=*)
            OUTPUT_FORMAT="${1#*=}"
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        --no-health)
            CHECK_HEALTH=false
            shift
            ;;
        --timeout=*)
            TIMEOUT="${1#*=}"
            shift
            ;;
        --help|-h)
            usage
            ;;
        -*)
            log_error "Unknown option: $1"
            usage
            ;;
        *)
            if [[ -z "${CLUSTER_NAME}" ]]; then
                CLUSTER_NAME="$1"
            elif [[ "$2" == "default" ]]; then
                NAMESPACE="$1"
            fi
            shift
            ;;
    esac
done

# Validate required dependencies
if [[ "${OUTPUT_FORMAT}" == "yaml" ]] && ! command -v yq >/dev/null 2>&1; then
    log_error "yq is required for YAML output format"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    log_error "jq is required for JSON processing"
    exit 1
fi

main "$@"
