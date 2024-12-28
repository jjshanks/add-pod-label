#!/bin/bash

set -euo pipefail

# Use a high port number that doesn't require root
LOCAL_PORT=18443

# Cleanup function to handle multiple cleanup tasks
cleanup() {
    echo "Cleaning up test resources..."
    # Kill port forwarding if it exists
    if [ -n "${PORT_FORWARD_PID:-}" ]; then
        echo "Stopping port forwarding process..."
        kill $PORT_FORWARD_PID || true
        wait $PORT_FORWARD_PID 2>/dev/null || true
    fi
    # Delete test resources
    echo "Deleting test deployments..."
    kubectl delete -f test/e2e/manifests/test-deployment.yaml --ignore-not-found
}

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

# Wait for port forwarding to be ready
echo "Waiting for port forwarding to be established..."
for i in {1..10}; do
    if nc -z localhost $LOCAL_PORT; then
        break
    fi
    if [ $i -eq 10 ]; then
        echo "ERROR: Port forwarding failed to establish"
        exit 1
    fi
    sleep 1
done

echo "Testing liveness endpoint..."
if ! curl -sk https://localhost:$LOCAL_PORT/healthz | grep -q "OK"; then
    echo "ERROR: Liveness check failed"
    exit 1
fi

echo "Testing readiness endpoint..."
if ! curl -sk https://localhost:$LOCAL_PORT/readyz | grep -q "OK"; then
    echo "ERROR: Readiness check failed"
    exit 1
fi

echo "Health checks passed successfully!"

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

echo "All tests passed successfully!"
exit 0