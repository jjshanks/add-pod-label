#!/bin/bash

set -euo pipefail

# Source common test utilities
source "$(dirname "$0")/utils.sh"

# Use a high port number that doesn't require root
LOCAL_PORT=18443

# Set up cleanup on script exit
trap cleanup EXIT

echo "Applying test deployments..."
kubectl apply -f test/e2e/manifests/test-deployment.yaml

echo "Waiting for deployments to be available..."
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test-no-label

echo "Checking webhook pod health status..."
WEBHOOK_POD=$(kubectl get pods -n webhook-test -l app=pod-label-webhook -o jsonpath='{.items[0].metadata.name}')

# Wait for pod to be ready
echo "Waiting for webhook pod to be ready..."
kubectl wait --for=condition=Ready --timeout=60s -n webhook-test pod/$WEBHOOK_POD

# Get the service port
WEBHOOK_PORT=$(kubectl get service pod-label-webhook -n webhook-test -o jsonpath='{.spec.ports[0].port}')

# Setup port forwarding
echo "Setting up port forwarding..."
kubectl port-forward -n webhook-test service/pod-label-webhook $LOCAL_PORT:$WEBHOOK_PORT &
PORT_FORWARD_PID=$!

# Wait for port forwarding to be established
if ! wait_for_port $LOCAL_PORT; then
    echo "ERROR: Port forwarding failed to establish"
    exit 1
fi

# Verify the webhook is labeling pods correctly
echo "Checking for expected label presence..."
if ! kubectl get pods -n default -l app=integ-test,hello=world --no-headers 2>/dev/null | grep -q .; then
    echo "ERROR: Label 'hello=world' not found when it should be present"
    exit 1
fi

echo "Checking for expected label absence..."
if kubectl get pods -n default -l app=integ-test-no-label,hello=world --no-headers 2>/dev/null | grep -q .; then
    echo "ERROR: Label 'hello=world' found when it should not be present"
    exit 1
fi

echo "Testing metrics endpoint..."
metrics_output=$(curl -sk https://localhost:$LOCAL_PORT/metrics)
if [ $? -ne 0 ]; then
    echo "ERROR: Failed to fetch metrics"
    exit 1
fi

echo "Initial metrics check..."
echo "Got $(echo "$metrics_output" | wc -l) lines of metrics"

# Verify metrics
echo "Verifying metrics..."

# Wait a moment for metrics to be available
sleep 5

# Check readiness status first
if ! check_metric "pod_label_webhook_readiness_status" "" "1" "Webhook readiness" "$LOCAL_PORT"; then
    exit 1
fi

# Check request metrics
if ! check_metric "pod_label_webhook_requests_total" 'method="POST",path="/mutate",status="200"' "1" "Successful mutate requests" "$LOCAL_PORT"; then
    exit 1
fi

# Check label operations
if ! check_metric "pod_label_webhook_label_operations_total" 'namespace="default",operation="success"' "1" "Successful label operations" "$LOCAL_PORT"; then
    exit 1
fi

if ! check_metric "pod_label_webhook_label_operations_total" 'namespace="default",operation="skipped"' "1" "Skipped label operations" "$LOCAL_PORT"; then
    exit 1
fi

echo "All tests passed successfully!"