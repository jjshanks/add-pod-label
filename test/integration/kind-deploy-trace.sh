#!/bin/bash

set -euo pipefail

# Set default cluster name if not provided
CLUSTER_NAME=${KIND_CLUSTER_NAME:-webhook-test}
NAMESPACE=${NAMESPACE:-webhook-test}
IMAGE_NAME=${IMAGE_NAME:-ghcr.io/jjshanks/pod-label-webhook}
VERSION=${VERSION:-latest}

kind export kubeconfig --name ${CLUSTER_NAME}
kind load docker-image ${IMAGE_NAME}:${VERSION} --name ${CLUSTER_NAME}

# Apply webhook configuration
kubectl apply -f test/e2e/manifests/webhook.yaml
kubectl wait --for=condition=Ready --timeout=60s -n ${NAMESPACE} certificate/pod-label-webhook-cert

# Apply OpenTelemetry collector and test services
kubectl apply -f test/e2e/manifests/test-deployment-trace.yaml

# Wait for OpenTelemetry collector
kubectl wait --for=condition=Available --timeout=60s -n webhook-test deployment/otel-collector

# Apply deployment with tracing enabled
kubectl apply -f test/e2e/manifests/deployment-trace.yaml
kubectl wait --for=condition=Available --timeout=60s -n ${NAMESPACE} deployment/pod-label-webhook