#!/bin/bash

# This script is used to build container images of history server components in
# the buildkite test-historyserver-e2e step.

# NOTE: For now, only the collector is built and tested.

# TODO(jwj): Skip building if starting from ray ci release automation.
make localimage-collector &&
kind load docker-image collector:v0.1.0
