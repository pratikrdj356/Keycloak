#!/usr/bin/env bash
# Tears down the Pulumi-managed resources and deletes the local cluster.
set -euo pipefail
cd "$(dirname "$0")/.."

export PULUMI_BACKEND_URL="${PULUMI_BACKEND_URL:-file://./.pulumi-state}"
export PULUMI_CONFIG_PASSPHRASE="${PULUMI_CONFIG_PASSPHRASE:-local-dev-only}"

pulumi destroy --yes --stack dev || true
k3d cluster delete keycloak-local || true

echo "==> Cluster and stack removed."
