#!/bin/bash

set -euo pipefail

# Source common test utilities
source "$(dirname "$0")/utils.sh"

# Use a high port number that doesn't require root
LOCAL_PORT=18443
OTEL_PORT=14317

# Set up cleanup on script exit
trap cleanup EXIT

echo "Applying OpenTelemetry collector and test deployments..."
kubectl apply -f test/e2e/manifests/test-deployment-trace.yaml

echo "Waiting for OpenTelemetry collector to start..."
kubectl wait --for=condition=Available --timeout=60s -n webhook-test deployment/otel-collector

echo "Applying test deployments..."
kubectl apply -f test/e2e/manifests/test-deployment.yaml

echo "Waiting for test deployments to be available..."
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test-no-label
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test-trace

echo "Checking webhook pod health status..."
WEBHOOK_POD=$(kubectl get pods -n webhook-test -l app=pod-label-webhook -o jsonpath='{.items[0].metadata.name}')

# Wait for pod to be ready
echo "Waiting for webhook pod to be ready..."
kubectl wait --for=condition=Ready --timeout=60s -n webhook-test pod/$WEBHOOK_POD

# Get the service port
WEBHOOK_PORT=$(kubectl get service pod-label-webhook -n webhook-test -o jsonpath='{.spec.ports[0].port}')

# Setup port forwarding for webhook
echo "Setting up port forwarding for webhook..."
kubectl port-forward -n webhook-test service/pod-label-webhook $LOCAL_PORT:$WEBHOOK_PORT &
PORT_FORWARD_PID=$!

# Wait for port forwarding to be established
if ! wait_for_port $LOCAL_PORT; then
    echo "ERROR: Webhook port forwarding failed to establish"
    exit 1
fi

# Setup port forwarding for otel-collector
echo "Setting up port forwarding for otel-collector..."
OTEL_COLLECTOR_POD=$(kubectl get pods -n webhook-test -l app=otel-collector -o jsonpath='{.items[0].metadata.name}')
kubectl port-forward -n webhook-test pod/$OTEL_COLLECTOR_POD $OTEL_PORT:4317 &
OTEL_PORT_FORWARD_PID=$!

# Wait for port forwarding to be established
if ! wait_for_port $OTEL_PORT; then
    echo "ERROR: OpenTelemetry collector port forwarding failed to establish"
    exit 1
fi

# Check if webhook logs include tracing initialization messages
echo "Checking for tracing initialization in logs..."
if ! kubectl logs -n webhook-test $WEBHOOK_POD | grep -q "OpenTelemetry tracing initialized"; then
    echo "ERROR: Tracing initialization not found in logs"
    kubectl logs -n webhook-test $WEBHOOK_POD
    exit 1
fi

# Check that OpenTelemetry collector is receiving spans
echo "Creating test pod to generate spans..."
# Create a pod that will trigger the webhook 
kubectl run trace-test-pod --image=busybox --restart=Never -- sleep 300

# Wait for pod to be created
sleep 5

# Check collector logs for received spans
echo "Checking OpenTelemetry collector logs for spans..."
SPAN_FOUND=false
MAX_ATTEMPTS=10
ATTEMPT=1

while [ $ATTEMPT -le $MAX_ATTEMPTS ]; do
    if kubectl logs -n webhook-test $OTEL_COLLECTOR_POD | grep -q "handle_mutate"; then
        SPAN_FOUND=true
        break
    fi
    echo "Attempt $ATTEMPT: Waiting for spans to appear in collector logs..."
    sleep 3
    ATTEMPT=$((ATTEMPT + 1))
done

if [ "$SPAN_FOUND" = false ]; then
    echo "ERROR: No webhook spans found in OpenTelemetry collector logs"
    kubectl logs -n webhook-test $OTEL_COLLECTOR_POD
    exit 1
fi

echo "Tracing verified - spans found in collector logs!"

# Delete the test pod
kubectl delete pod trace-test-pod --wait=false

# Done
echo "All tracing tests passed successfully!"

# cleanup function defined in utils.sh will handle stopping port forwarding