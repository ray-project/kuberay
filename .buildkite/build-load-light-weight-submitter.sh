#!/bin/bash

# This script is used to build and load the light-weight submitter image into the kind cluster.
IMG=kuberay/submitter:nightly make docker-image-rayjobsubmitter &&
    kind load docker-image kuberay/submitter:nightly
