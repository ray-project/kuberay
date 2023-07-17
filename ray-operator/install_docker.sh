#!/bin/bash

export DEBIAN_FRONTEND=noninteractive
export TZ=America/Los_Angeles

locale -a
locale-gen en_US.UTF-8
export LC_ALL=en_US.UTF-8
export LANG=en_US.UTF-8

apt-get update -qq && apt-get upgrade -qq
apt-get install -y -qq \
    curl python-is-python3 git build-essential \
    sudo unzip unrar apt-utils dialog tzdata wget rsync \
    language-pack-en tmux

curl -o- https://get.docker.com | sh
