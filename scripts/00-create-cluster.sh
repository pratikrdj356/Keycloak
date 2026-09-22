#!/usr/bin/env bash
# Provisions a local Kubernetes cluster using k3d (Rancher's lightweight k3s-in-Docker
# distribution) and installs the ingress-nginx controller that fronts Keycloak.
set -euo pipefail

CLUSTER_NAME="keycloak-local"

command -v docker >/dev/null 2>&1 || { echo "Docker is required and was not found." >&2; exit 1; }

if ! command -v k3d >/dev/null 2>&1; then
  echo "==> Installing k3d..."
  curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
fi

if k3d cluster list | grep -q "^${CLUSTER_NAME} "; then
  echo "==> Cluster '${CLUSTER_NAME}' already exists, skipping creation."
else
  echo "==> Creating k3d cluster '${CLUSTER_NAME}'..."
  k3d cluster create "${CLUSTER_NAME}" \
    --agents 2 \
    --port "80:80@loadbalancer" \
    --port "443:443@loadbalancer" \
    --k3s-arg "--disable=traefik@server:0"
fi

kubectl config use-context "k3d-${CLUSTER_NAME}"

echo "==> Installing ingress-nginx (Traefik disabled to avoid a port conflict)..."
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx --force-update >/dev/null
helm repo update >/dev/null
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx --create-namespace \
  --set controller.service.type=LoadBalancer \
  --wait --timeout 180s

echo "==> Cluster is ready. Context: k3d-${CLUSTER_NAME}"
echo "==> Add this to /etc/hosts before browsing to Keycloak:"
echo "    127.0.0.1 keycloak.local"
