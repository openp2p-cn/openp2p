# use docker image

This docker image is about 10m in size and can be used directly or built by yourself

## 1. Run the docker command

```bash

> /home/openp2p/config.json && docker run --restart=on-failure:3 --network host --name myopenp2p1 -d -v /home/openp2p/config.json:/config.json -e HostName= "Any name" -e token="your token" bd111/openp2p:3.4.0_v1

````

Before running the command, modify two parameters, one is the token and the other is the HostName. The token is the one you applied for on the openp2p official website, and the HostName is a name you arbitrarily named.

First, go to the openp2p official website (<https://console.openp2p.cn/profile>) to register an account, and the system will automatically generate a token. The specific operation is shown in the following figure:

![image](/doc/images/get_token.png)

Suppose your token is: 1234567890 and your HostName is: openp2p14, then your running command is:

```bash
> /home/openp2p/config.json && docker run --restart=on-failure:3 --network host --name myopenp2p14 -d -v /home/openp2p/config.json:/config.json -e HostName= "openp2p14" -e token="1234567890" bd111/openp2p:3.4.0_v1

````

Copy the code to the terminal to run, if there is no error, then your openp2p is already running.

![image](/doc/images/docker_run_openp2p.png)

We can now go to the openp2p official website (<https://console.openp2p.cn/profile>) to see if our node is up and running.

![image](/doc/images/openp2p_is_run.png)

## 2. Example

Our openp2p is up and running, now we can use it. Let's take an example to illustrate. Suppose we have an intranet server, its ip is 192.168.1.5 (if you want to access the local service, then the ip is localhost), the service port is 5000, we want to make the external network another A machine that has deployed openp2p to access this server, then we operate as shown below:

![image](/doc/images/openp2p_app.png)

Then at this time, we can access: <http://127.0.0.1:6666/> on the external network machine.

## 3. Build your own docker image

Assuming the name of our dockerfile is openp2p.dockerfile, then we can build the image with the following command:

```bash
docker build -f openp2p.dockerfile -t openp2p:busybox_3.4.0_cs .

````
