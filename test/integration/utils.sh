#!/bin/bash

# test/integration/utils.sh
# Common utility functions for integration tests

# Extract metric value using grep and awk
get_metric_value() {
    local metric_name=$1
    local labels=$2
    local metrics_output=$3
    local value
    
    if [ -z "$labels" ]; then
        # For metrics without labels (like gauges)
        value=$(echo "$metrics_output" | grep "^$metric_name " | awk '{print $2}')
    else
        # For metrics with labels
        value=$(echo "$metrics_output" | grep "^$metric_name{${labels}}" | awk '{print $2}')
    fi
    
    # If no value found, print the matching lines for debugging
    if [ -z "$value" ]; then
        echo "DEBUG: Lines matching ${metric_name}:" >&2
        echo "$metrics_output" | grep "^${metric_name}" >&2
    fi
    
    echo "$value"
}

# Check metric with retries
check_metric() {
    local metric_name=$1
    local labels=$2
    local expected_min=$3
    local description=$4
    local metrics_port=$5
    local max_attempts=10
    local attempt=1
    local value
    local metrics_output

    while [ $attempt -le $max_attempts ]; do
        metrics_output=$(curl -sk https://localhost:${metrics_port}/metrics 2>/dev/null)
        value=$(get_metric_value "$metric_name" "$labels" "$metrics_output")
        
        if [ -n "$value" ]; then
            if awk "BEGIN {exit !($value >= $expected_min)}"; then
                echo "✓ $description verified (value: $value)"
                return 0
            fi
        fi
        
        echo "Attempt $attempt: Waiting for $description (current: ${value:-none}, expected min: $expected_min)"
        sleep 5
        attempt=$((attempt + 1))
    done
    
    echo "ERROR: Failed to verify $description after $max_attempts attempts"
    echo "Current metrics output:"
    echo "$metrics_output"
    return 1
}

# Wait for port to be available
wait_for_port() {
    local port=$1
    local max_attempts=${2:-10}
    local attempt=1

    echo "Waiting for port $port to be available..."
    while [ $attempt -le $max_attempts ]; do
        if nc -z localhost "$port"; then
            echo "Port $port is available"
            return 0
        fi
        echo "Attempt $attempt: Port $port not available yet"
        sleep 2
        attempt=$((attempt + 1))
    done

    echo "ERROR: Port $port not available after $max_attempts attempts"
    return 1
}

# Clean up resources and handle interrupts
cleanup() {
    echo "Cleaning up test resources..."
    # Kill port forwarding if it exists
    if [ -n "${PORT_FORWARD_PID:-}" ]; then
        echo "Stopping webhook port forwarding process..."
        kill $PORT_FORWARD_PID || true
        wait $PORT_FORWARD_PID 2>/dev/null || true
    fi
    
    # Kill otel port forwarding if it exists
    if [ -n "${OTEL_PORT_FORWARD_PID:-}" ]; then
        echo "Stopping OpenTelemetry collector port forwarding process..."
        kill $OTEL_PORT_FORWARD_PID || true
        wait $OTEL_PORT_FORWARD_PID 2>/dev/null || true
    fi
    
    # Delete test resources
    echo "Deleting test deployments..."
    kubectl delete -f test/e2e/manifests/test-deployment.yaml --ignore-not-found
    kubectl delete -f test/e2e/manifests/test-deployment-trace.yaml --ignore-not-found
    
    # Delete additional test pods if they exist
    kubectl delete pod trace-test-pod --ignore-not-found --wait=false
}