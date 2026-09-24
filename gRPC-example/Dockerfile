# docker build . -t twilight1998/evalai-grpc-test:latest
# docker run -it --rm -p 8001:8001 twilight1998/evalai-grpc-test:latest bash


# Use Anyscale base image
FROM rayproject/ray:2.7.0

# Install dependencies
RUN pip install --upgrade pip && pip install --upgrade protobuf

WORKDIR /home/ray

# Copy local code including protobuf and service definitions into docker image
COPY . /home/ray

# Add working directory into python path so they are importable
ENV PYTHONPATH=/home/ray