#!/bin/bash
EXPECTED_RUNNING=3
EXPECTED_PENDING=6
TIMEOUT=300
INTERVAL=5
GRACE_PERIOD=10
elapsed=0

# Wait for the desired number of Running pods
while [ "$(kubectl get pods -l ray.io/cluster=raycluster-topology-test -o jsonpath='{.items[*].status.phase}' | grep -o 'Running' | wc -l)" -lt "$EXPECTED_RUNNING" ]; do
  echo "Waiting for $EXPECTED_RUNNING pods to be in Running state..."
  kubectl get pods -o wide
  echo "------------------------------------------------------------"
  sleep $INTERVAL
  elapsed=$((elapsed + INTERVAL))
  if [ "$elapsed" -ge "$TIMEOUT" ]; then
    echo "Timeout reached. Not all expected pods are running."
    exit 1
  fi
done

echo "$EXPECTED_RUNNING pods are running. Checking for pending pods with a $GRACE_PERIOD second grace period..."

# Wait for the grace period to account for latecomers
sleep $GRACE_PERIOD

ACTUAL_PENDING=$(kubectl get pods -l ray.io/cluster=raycluster-topology-test -o jsonpath='{.items[*].status.phase}' | grep -o 'Pending' | wc -l)
echo "Number of actual pending pods: $ACTUAL_PENDING (expected: $EXPECTED_PENDING)"

# Check if the actual number of pending pods matches the expected number.
if [ "$ACTUAL_PENDING" -eq "$EXPECTED_PENDING" ]; then
  echo "Topology spread constraints validated successfully. Test passed."
  kubectl delete rayclusters raycluster-topology-test
  exit 0
else
  echo "Unexpected number of pending pods. Test failed."
  exit 1
fi
