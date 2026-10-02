#!/usr/bin/env bash
# Tear down the Federated RayCluster local development environment.
set -euo pipefail

echo "Deleting kind clusters..."
kind delete cluster --name frc-primary  || true
kind delete cluster --name frc-member-a || true
kind delete cluster --name frc-member-b || true

echo "Done."
