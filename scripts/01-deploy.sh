#!/usr/bin/env bash
# Runs the Pulumi Go program against the cluster created by 00-create-cluster.sh.
set -euo pipefail

command -v pulumi >/dev/null 2>&1 || { echo "Pulumi CLI is required. See https://www.pulumi.com/docs/install/" >&2; exit 1; }
command -v go >/dev/null 2>&1     || { echo "Go 1.21+ is required." >&2; exit 1; }

cd "$(dirname "$0")/.."

# Local, file-based state backend so this is fully self-contained (no Pulumi Cloud account needed).
export PULUMI_BACKEND_URL="${PULUMI_BACKEND_URL:-file://./.pulumi-state}"
export PULUMI_CONFIG_PASSPHRASE="${PULUMI_CONFIG_PASSPHRASE:-local-dev-only}"

mkdir -p .pulumi-state

go mod tidy

pulumi stack select dev --create --non-interactive
pulumi up --yes --stack dev

echo ""
echo "==> Deployment complete. Retrieve credentials with:"
echo "    pulumi stack output keycloakAdminUser --stack dev"
echo "    pulumi stack output keycloakAdminPassword --show-secrets --stack dev"
echo "    pulumi stack output keycloakUrl --stack dev"
