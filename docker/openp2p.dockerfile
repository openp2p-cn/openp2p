# 运行容器命令: > /home/openp2p/config.json && docker run --restart=on-failure:3 --network host --name myopenp2p1 -d -v /home/openp2p/config.json :/config.json -e HostName="随便一个名字" -e token="你的token"  bd111/openp2p:3.4.0_v1
# 说明：HostName是你的主机的名字，可随意，token是你的token
# 说明：config.json是你的配置文件，可自行修改，也可不修改，不修改的话，会使用默认配置,在主机/home/openp2p/config下可以编辑

# Run container command: > /home/openp2p/config.json && docker run --restart=on-failure:3 --webhost --name myopenp2p1 -d -v /home/openp2p/config.json :/config.json -e HostName="Any name" -e token="your token." ” bd111/openp2p：3.4.0_v1
# Note: HostName is the name of your host, feel free, the token is your token
# Note: config.json is your configuration file, you can modify it yourself, or not, if you do not modify, it will use the default configuration, which can be edited under host/home/openp2p/config


FROM alpine:latest as builder
LABEL version="1.0"
ENV HostName="openp2p"  token="1234567890" 
RUN  apk add curl &&\
    curl -k -o install.sh "https://openp2p.cn/download/v1/latest/install.sh" &&\
    chmod +x install.sh &&\
    ./install.sh --token 1234567890


FROM  busybox
COPY --from=builder /openp2p /


CMD ./openp2p -d -node $HostName -token $token 