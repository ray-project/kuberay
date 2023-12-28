FROM rayproject/ray:nightly-py38-cpu

# Install podman
RUN sudo apt update && \
    sudo apt-get install -y curl && \
    sudo apt install software-properties-common uidmap -y && \
    sudo sh -c "echo 'deb http://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_20.04/ /' > /etc/apt/sources.list.d/devel:kubic:libcontainers:stable.list" && \
    sudo apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 4D64390375060AA4 && \
    sudo apt-get update && \
    sudo apt-get install podman -y

# Modify ray source code with working container runtime plugin
RUN curl -o /home/ray/anaconda3/lib/python3.8/site-packages/ray/_private/runtime_env/container.py https://raw.githubusercontent.com/ray-project/ray/5cc42e65c1ef9cf75f0909929624a28607a1d7f3/python/ray/_private/runtime_env/container.py
RUN curl -o /home/ray/anaconda3/lib/python3.8/site-packages/ray/_private/runtime_env/context.py https://raw.githubusercontent.com/ray-project/ray/5cc42e65c1ef9cf75f0909929624a28607a1d7f3/python/ray/_private/runtime_env/context.py
