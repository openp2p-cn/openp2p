FROM  alpine:3.18.2

# Replace the default Alpine repositories with Aliyun mirrors
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add --no-cache ca-certificates iptables && \
    rm -rf /tmp/* /var/tmp/* /var/cache/apk/* /var/cache/distfiles/*

COPY get-client.sh /
ARG VERSION
LABEL version=${VERSION}
# ARG DOCKER_VER="latest"
RUN echo $TARGETPLATFORM && chmod +x /get-client.sh && ./get-client.sh

ENTRYPOINT ["/usr/local/openp2p/openp2p"]
