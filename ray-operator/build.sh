#!/bin/bash

SHARED_DIR=/shared

rm -rf $SHARED_DIR/*
cp -rf ./docker $SHARED_DIR
pushd $SHARED_DIR/docker
docker build -t "$1" .
popd