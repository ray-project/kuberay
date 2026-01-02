ARG TARGETOS
ARG TARGETARCH

FROM --platform=$BUILDPLATFORM golang:1.25.1 as builder
ENV GOPROXY=https://goproxy.cn,direct
ARG BUILD_RAYSERVER_DASHBOARD

RUN if [ "$BUILD_RAYSERVER_DASHBOARD" = "yes" ] ; then \
 curl -o install.sh https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh && chmod +x install.sh && ./install.sh && /bin/bash -c "source $HOME/.nvm/nvm.sh && nvm install 14 && nvm use 14" ;\
else \
 echo "$BUILD_RAYSERVER_DASHBOARD not yes, no need install nvm"; \
fi

WORKDIR /historyserver
COPY . .

RUN if [ "$BUILD_RAYSERVER_DASHBOARD" = "yes" ] ; then \
 /bin/bash -c "source $HOME/.nvm/nvm.sh && cd dashboard/v2.51.0/client && npm ci && npm run build" ;\
else \
 mkdir -p dashboard/v2.51.0/client/build ;\
 echo "do not npm run build"; \
fi

RUN make build GOOS=${TARGETOS} GOARCH=${TARGETARCH}

FROM ubuntu:22.04

RUN apt-get update && apt-get upgrade -y && rm -rf /var/cache/apt/ && apt-get install -y ca-certificates

COPY --from=builder /historyserver/output/bin/historyserver /usr/local/bin/historyserver
# COPY --from=builder /historyserver/dashboard/v2.51.0/client/build /dashboard/v2.51.0/client/build
# COPY --from=builder /historyserver/dashboard/homepage /dashboard/homepage
