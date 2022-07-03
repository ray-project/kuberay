ARG BASE_IMAGE="rayproject/ray:1.12.0"
FROM ${BASE_IMAGE}

# We specify different arguments to build different targets
# dimensions: python version, cpu/gpu, core/ml package, ray version

# KubeRay: workspace service related
ARG NB_TOKEN=""
ENV NB_PREFIX /

LABEL maintainer="Jiaxin Shan <jiaxin.shan@bytedance.com>"

# KubeRay: Install some required packages that ray base-deps doesn't install.
# https://github.com/ray-project/ray/blob/29ecffe8051e852f96070d297fd2c64a98c1ecf5/docker/base-deps/Dockerfile#L30-L42
# Remove kubernetes source base image add earlier. This is for CN because google is not reachable.
RUN sudo rm /etc/apt/sources.list.d/kubernetes.list
RUN sudo apt-get update && \
    sudo apt-get install -yq --no-install-recommends \
    curl \
    unzip \
    vim \
    zip \
    && sudo apt-get clean \
    && sudo apt-get autoremove -y \
    && sudo rm -rf /var/lib/apt/lists/*


# KubeRay: Install Nodejs for jupyterlab-manager
# TODO: combine with above and see whether jupyter lab needs nodejs anymore.
RUN curl -sL https://deb.nodesource.com/setup_12.x | sudo -E bash -
RUN sudo apt-get update && sudo apt-get purge nodejs && sudo apt-get install -yq --no-install-recommends \
    nodejs && \
    sudo apt-get clean && \
    sudo rm -rf /var/lib/apt/lists/*

# KubeRay: Customize screen and use bash instead of sh
SHELL ["/bin/bash", "-c"]
COPY bashrc /etc/bash.bashrc
RUN sudo chmod a+rwx /etc/bash.bashrc

##################################################################################

# NOTE: Beyond this point be careful of breaking out
# or otherwise adding new layers with RUN, chown, etc.
# The image size can grow significantly.

# 1. Install Jupyter core packages
# Pin ipykernel and nbformat; see https://github.com/ipython/ipykernel/issues/422
ENV JUPYTERLAB_DIR $HOME/anaconda3/share/jupyter/lab

# Fetch dependencies from internal registry.
RUN sudo npm install --global yarn

# Install Jupyter Notebook, Lab
# Generate a notebook server config
# Cleanup temporary files
# Do all this in a single RUN command to avoid duplicating all of the files across image layers
# Note: versions come from existing installation and then we pin package versions
RUN $HOME/anaconda3/bin/conda install --quiet --yes \
    'notebook=6.4.8' \
    'jupyterlab=3.3.2' \
    'ipywidgets=7.6.5' \
    'qtconsole=5.3.0' \
    ##jupyter-console && \
    ##jupyter_packaging && \
    'tini=0.18.0' && \
    conda clean --all -f -y && \
    npm cache clean --force && \
    jupyter notebook --generate-config && \
    rm -rf $HOME/anaconda3/share/jupyter/lab/staging && \
    rm -rf $HOME/.cache/yarn

# 2.1 Install JupyterLab extensions - server extensions
RUN $HOME/anaconda3/bin/pip --no-cache-dir install \
    jupyter_http_over_ws \
    jupyterlab-git \
    # TODO: enable language support later.
    # jupyter-lsp \
    # python-language-server[all] \
    --index-url=${PYTHON_INDEX_URL}

# 2.2 Install JupyterLab extensions - npm packages without server extensions
# TODO: Here we install our own in-house extensions

# 3. Enable Jupyter extentions
# Allows running Jupyter notebooks that use a WebSocket to proxy HTTP traffic.
RUN jupyter serverextension enable --py --sys-prefix jupyter_http_over_ws

# 4. Rebuild lab extentions
RUN jupyter lab build

EXPOSE 8888
ENV NB_DEFAULT_PORT 8888

# KubeRay: Configure container startup
# jupyter will report error if we use ARG for port, it will be rendered as string instead of int
ENTRYPOINT ["tini", "--"]
CMD ["bash","-c", "jupyter lab --notebook-dir=${HOME} --ip=0.0.0.0 --no-browser --allow-root --port=${NB_DEFAULT_PORT} --NotebookApp.token=${NB_TOKEN} --NotebookApp.password='' --NotebookApp.allow_origin='*' --NotebookApp.base_url=${NB_PREFIX}"]
