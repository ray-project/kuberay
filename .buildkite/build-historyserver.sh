#!/bin/bash

make docker-image &&
kind load docker-image collector:v0.1.0
