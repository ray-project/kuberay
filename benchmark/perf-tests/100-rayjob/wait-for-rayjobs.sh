#!/bin/bash

expect_succeeded=100
echo "waiting for $expect_succeeded RayJobs to be completed successfully"

while true; do
  num_succeeded=$(kubectl get rayjob -A -l perf-test=rayjob-pytorch-mnist  -o jsonpath='{range .items[*]}{.metadata.name} {.status.jobStatus}{"\n"}' | grep -c SUCCEEDED)
  echo "$num_succeeded RayJobs completed..."

  if [[ "$num_succeeded" == "$expect_succeeded" ]]; then
     break;
  fi

  sleep 5
done

while true; do
  num_succeeded=$(kubectl get rayjob -A -l perf-test=ray-data-image-resize -o jsonpath='{range .items[*]}{.metadata.name} {.status.jobStatus}{"\n"}' | grep -c SUCCEEDED)
  echo "$num_succeeded RayJobs completed..."

  if [[ "$num_succeeded" == "$expect_succeeded" ]]; then
     break;
  fi

  sleep 5
done

echo "$num_succeeded RayJobs completed!"
