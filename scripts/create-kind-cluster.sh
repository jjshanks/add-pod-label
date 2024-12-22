#!/bin/bash

set -euo pipefail

# Set default cluster name if not provided
CLUSTER_NAME=${KIND_CLUSTER_NAME:-webhook-test}
NAMESPACE=${NAMESPACE:-webhook-test}

echo "Creating kind cluster: ${CLUSTER_NAME}"

# Create kind cluster using external config
kind create cluster --name "${CLUSTER_NAME}" --config tests/manifests/kind-config.yaml

echo "Installing cert-manager..."

# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.4/cert-manager.yaml

echo "Waiting for cert-manager deployments to be available..."

kubectl wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager
kubectl wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-cainjector
kubectl wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-webhook

kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | kubectl apply -f -

echo "Kind cluster ${CLUSTER_NAME} is ready with cert-manager installed!"