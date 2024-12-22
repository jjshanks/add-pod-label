#!/bin/bash

set -euo pipefail

kubectl apply -f tests/manifests/test-deployment.yaml
kubectl wait --for=condition=Available --timeout=60s -n default deployment/integ-test

if kubectl get pods -n default -l app=integ-test,hello=world --no-headers 2>/dev/null | grep -q .; then
    echo "Label exists"
    exit 0
else
    echo "Label not found"
    exit 1
fi