#!/bin/bash

# Suppress interactive prompts
export DEBIAN_FRONTEND=noninteractive
export TZ=America/Los_Angeles

apt-get update -qq && apt-get upgrade -qq
apt-get install -y -qq curl

curl -o- https://get.docker.com | sh
