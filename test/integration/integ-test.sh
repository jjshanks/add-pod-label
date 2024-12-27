#!/bin/bash

set -euo pipefail

# Cleanup function
cleanup() {
    echo "Cleaning up test resources..."
    kubectl delete -f test/e2e/manifests/test-deployment.yaml --ignore-not-found
}

# Set up cleanup on script exit
trap cleanup EXIT

echo "Applying test deployments..."
kubectl apply -f test/e2e/manifests/test-deployment.yaml

echo "Waiting for deployments to be available..."
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test-no-label

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