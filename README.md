# English|[中文](/README-ZH.md)  
Website: [openp2p.cn](https://openp2p.cn)
## What is OpenP2P
It is an open source, free, and lightweight P2P sharing network. Your devices will form a private P2P network, in which devices can directly access other members, or indirectly access through other members forwarding data.
If the private network cannot complete the communication, it will go to the public P2P network to find a shared node to assist in the communication. Compared with the BT network used to share files, the OpenP2P network is used to share bandwidth.
Our goal is to make full use of bandwidth, use shared nodes to relay data, and build a common infrastructure for remote connections.
## Why OpenP2P
### 1. Free
Totaly free, fullfills most of users(especially free-rider). Unlike other similar products, OpenP2p doesn't need a server with public IP, and doesn't need to pay for services.By understanding its principle, you can understand why it can be done for free.
### 2. Share
Your devices will form a private P2P network, share bandwidth between them, and provide network data forwarding services.
When there is no node that can provide forwarding services in your private P2P network, you will try to find forwarding nodes in the public P2P network.
10mbps is its default setting of share speed limit. Only when you have shared their nodes, you are allowed to use others' shared nodes. This is very fair, and it is also the original intention of this project.
We recommend that you join a shared network in a place with sufficient bandwidth (such as an office or home with 100M optical fiber).
If you are not willing to contribute any node to the OpenP2P share network, please refer to the [usage](/USAGE.md) for your own setting.
### 3. Safe
The code is open source, the P2P tunnel uses TLS1.3+AES double encryption, and the shared node temporarily authorizes the use of the TOTP one-time password
  
[details](#Safety)

### 4. Lightweight
10MB filesize, Extremely low CPU usage. It could only runs at application layer, or uses kmod-tun/wintun driver for SDWAN.

### 5. Cross-platform
Benefit from lightweight, it easily supports most of major OS, like Windows, Linux, MacOS, also most of CPU architecture, like 386、amd64、arm、arm64、mipsle、mipsle64、mips、mips64、s390x、ppc64le.

### 6. Efficient
P2P direct connection lets your devices make good use of bandwidth.  Your device can be connected in any network environments, even supports NAT1-4 (Cone or Symmetric),UDP or TCP punching,UPNP,IPv6.  Relying on the excellent congestion algorithm of the Quic protocol, high bandwidth and low latency can be obtained in a bad network environment.

### 7. Integration
Your applicaiton can call OpenP2P with a few code to make any internal networks communicate with each other.

## Get Started
Just 4 simple steps to use.
Here's an example of remote work: connecting to an office Windows computer at home.
(Another quick started vedio https://www.bilibili.com/video/BV1Et4y1P7bF/)
### 1.Register
Go to <https://console.openp2p.cn> register a new user using email

   ![image](/doc/images/register_en.png)
### 2.Install
Download on local and remote computers and double-click to run, one-click installation (Windows user, please do not modify the file name after downloading in the browser!!!)

   ![image](/doc/images/install_en.png)

By default, Windows will block programs that have not been signed by the Microsoft's certificate, and you can select "Run anyway".

   ![image](/doc/images/win10warn_en.png)

   ![image](/doc/images/stillrun_en.png)
   
### 3.New Port ForWard (P2PApp)

![image](/doc/images/devices_en.png)

![image](/doc/images/newapp_en.png)

![image](/doc/images/newappedit_en.png)

### 4.Use Port ForWard (P2PApp)
You can see the P2P application you just created on the "MyHomePC2" device, just connect to the "local listening port" shown in the figure below.

![image](/doc/images/p2pappok_en.png)

On MyHomePC2, press Win+R and enter MSTSC to open the remote desktop, input `127.0.0.1:23389 /admin`

   ![image](/doc/images/mstscconnect_en.png)

   ![image](/doc/images/afterconnect_en.png)


## Usage
[Here](/USAGE.md) describes how to run manually

## Scenarios 
Especially suitable for large traffic intranet access.
>*  Remote work: Windows MSTSC, VNC and other remote desktops, SSH, various ERP systems in the intranet
>*  Remote access ERP systems in the intranet
>*  Remote access NAS: Manage a large number of videos and pictures
>*  Remote access camera
>*  Remote flashing phone
>*  Remotely data backup
---
## Overview Design
### Prototype
![image](/doc/images/prototype.png)
### Client architecture
![image](/doc/images/architecture.png)
### Port ForWard (P2PApp)
Port ForWard (P2PApp) is the most import concept in this project, one Port ForWard (P2PApp) is able to map the remote service(mstsc/ssh) to the local listening. The main job of re-development or restful API we provide is to manage Port ForWard (P2PApp).

![image](/doc/images/appdetail.png)

## Safety
The nodes which have joined the OpenP2P share network can vist each other by authentications. Shared nodes will only relay data, and others cannot access any resources in the intranet.

### 1. TLS1.3+AES
The communication data between the two nodes uses the industry's most secure TLS1.3 channel. The communication content will also use AES encryption, double security, the key is exchanged through the server. Effectively prevent man-in-the-middle attacks.

### 2. Will the shared node capture my data?
That's right, the relay node is naturally an man-in-middle, so AES encryption is added to ensure the security of the communication content. The relay node cannot obtain the plaintext.
### 3. How does the shared relay node verify the authority?
The server side has a scheduling model, which calculate  bandwith, ping value,stability and service duration to provide a well-proportioned service to every share node. It uses TOTP(Time-based One-time Password) with hmac-sha256 algorithem, its theory as same as the cellphone validation code or bank cipher coder.

## Build
go version 1.20 only (support win7)
cd root directory of the socure code and execute
```
make
```

build specified os and arch.  
All GOOS values:
```
"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris", "windows", "zos"
```
All GOARCH values:
```
"386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe", "loong64", "mips", "mips64", "mips64le", "mips64p32", "mips64p32le", "mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390", "s390x", "sparc", "sparc64", "wasm"
```

For example linux+amd64
```
export GOPROXY=https://goproxy.io,direct
go mod tidy
CGO_ENABLED=0 env GOOS=linux GOARCH=amd64 go build -o openp2p --ldflags '-s -w ' -gcflags '-l' -p 8 -installsuffix cgo ./cmd
```

## RoadMap
Short-Term:
1. ~~Support IPv6.~~(100%)
2. ~~Support auto run when system boot, setup system service.~~(100%)
3. ~~Provide free servers to some low-performance network.~~(100%)
4. ~~Build website, users can manage all Port ForWard (P2PApp) and devices via it. View devices' online status, upgrade, restart or CURD Port ForWard (P2PApp) .~~(100%)
5. Provide wechat official account, user can manage Port ForWard (P2PApp) nodes and deivce as same as website.
6. Provide WebUI on client side.
7. ~~Support private server, open source server program.~~(100%)
8. Optimize our share scheduling model for different network operators.
9. ~~Provide REST APIs and libary for secondary development.~~(100%)
10. ~~Support UDP at application layer, it is easy to implement but not urgent due to only a few applicaitons using UDP protocol.~~(100%)
11. ~~Support KCP protocol underlay, currently support Quic only. KCP focus on delay optimization,which has been widely used as game accelerator,it can sacrifice part of bandwidth to reduce timelag. ~~(100%)
12. ~~Support Android platform, let the phones to be mobile gateway.~~(100%)
13. ~~Support SMB Windows neighborhood.~~(100%)
14. ~~Direct connection on intranet, for testing.~~(100%)
15. ~~Support UPNP.~~(100%)
16. ~~Support Android~~(100%)
17. Support IOS

Long-Term:
1. Use blockchain technology to decentralize, so that users who share equipment have benefits, thereby promoting more users to share, and achieving a positive closed loop.
2. Enterprise-level product can well manage large scale equipment and ACL.


## Contribute
If the items in TODO or ISSUE is your domain, or you have sepical good idea, welcome to join this OpenP2P project and contribute your code. When this project grows stronger, you will be the major outstanding contributors. That's cool.

## Contact
QQ Group: 16947733  

Email: openp2p.cn@gmail.com tenderiron@139.com

## Disclaimer
This project is open source for everyone to learn and use for free. It is forbidden to be used for illegal purposes. Any loss caused by improper use of this project or accident, this project and related personnel will not bear any responsibility. 

## Thanks
[![Powered by DartNode](https://dartnode.com/branding/DN-Open-Source-sm.png)](https://dartnode.com "Powered by DartNode - Free VPS for Open Source")