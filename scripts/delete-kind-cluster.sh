#!/bin/bash

set -euo pipefail

# Set default cluster name if not provided
CLUSTER_NAME=${KIND_CLUSTER_NAME:-webhook-test}

echo "Deleting kind cluster: ${CLUSTER_NAME}"

# Delete kind cluster
kind delete cluster --name "${CLUSTER_NAME}"