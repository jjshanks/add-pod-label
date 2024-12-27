#!/bin/bash

set -euo pipefail

# Set default cluster name if not provided
CLUSTER_NAME=${KIND_CLUSTER_NAME:-webhook-test}
NAMESPACE=${NAMESPACE:-webhook-test}
IMAGE_NAME=${IMAGE_NAME:-ghcr.io/jjshanks/pod-label-webhook}
VERSION=${VERSION:-latest}

kind export kubeconfig --name ${CLUSTER_NAME}
kind load docker-image ${IMAGE_NAME}:${VERSION} --name ${CLUSTER_NAME}

kubectl apply -f test/e2e/manifests/webhook.yaml
kubectl wait --for=condition=Ready --timeout=60s -n ${NAMESPACE} certificate/pod-label-webhook-cert
kubectl apply -f test/e2e/manifests/deployment.yaml
kubectl wait --for=condition=Available --timeout=60s -n ${NAMESPACE} deployment/pod-label-webhook