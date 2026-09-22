#!/usr/bin/env bash
# One-shot entrypoint: prerequisite check -> cluster -> deploy -> hosts entry -> credentials.
# Run this single script for the entire assignment setup: ./scripts/setup.sh
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Checking prerequisites..."
missing=0
for bin in docker kubectl helm pulumi go; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "    MISSING: $bin"
    missing=1
  fi
done
if [ "$missing" -eq 1 ]; then
  cat <<'EOF'

One or more required tools are missing. Install them first:
  - Docker:   https://www.docker.com/products/docker-desktop/  (launch it once before continuing)
  - kubectl:  brew install kubectl
  - helm:     brew install helm
  - Pulumi:   brew install pulumi   (or: curl -fsSL https://get.pulumi.com | sh)
  - Go 1.21+: brew install go

k3d itself is installed automatically by this script if missing.
EOF
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "Docker is installed but not running. Start Docker Desktop, wait for it to finish launching, then re-run this script." >&2
  exit 1
fi
echo "    All prerequisites present."

echo ""
echo "==> Step 1/3: provisioning the local Kubernetes cluster..."
./scripts/00-create-cluster.sh

echo ""
echo "==> Step 2/3: deploying Keycloak via Pulumi..."
./scripts/01-deploy.sh

echo ""
echo "==> Step 3/3: configuring local DNS (requires sudo for /etc/hosts)..."
if ! grep -qs "keycloak.local" /etc/hosts; then
  echo "127.0.0.1 keycloak.local" | sudo tee -a /etc/hosts >/dev/null
  echo "    Added 'keycloak.local' to /etc/hosts."
else
  echo "    'keycloak.local' already present in /etc/hosts, skipping."
fi

export PULUMI_BACKEND_URL="${PULUMI_BACKEND_URL:-file://./.pulumi-state}"
export PULUMI_CONFIG_PASSPHRASE="${PULUMI_CONFIG_PASSPHRASE:-local-dev-only}"

echo ""
echo "=================================================================="
echo " Setup complete."
echo "=================================================================="
echo " URL:      $(pulumi stack output keycloakUrl --stack dev)"
echo " Username: $(pulumi stack output keycloakAdminUser --stack dev)"
echo " Password: $(pulumi stack output keycloakAdminPassword --show-secrets --stack dev)"
echo "=================================================================="
echo " Open the URL above in a browser (accept the self-signed cert warning)."
echo " Tear down everything with: ./scripts/02-teardown.sh"
echo "=================================================================="
