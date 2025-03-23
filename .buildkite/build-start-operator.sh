#!/bin/bash

if [ "$IS_FROM_RAY_RELEASE_AUTOMATION" = 1 ]; then
    helm install kuberay/kuberay-operator --version 1.3.0;
fi

if [ "$IS_FROM_RAY_RELEASE_AUTOMATION" != 1 ];
    then IMG=kuberay/operator:nightly make docker-image &&
    kind load docker-image kuberay/operator:nightly &&
    IMG=kuberay/operator:nightly make deploy;
fi
