[English](/README.md)|中文
## OpenP2P是什么
它是一个开源、免费、轻量级的P2P共享网络。任何设备接入OpenP2P，随时随地访问它们。
我们的目标是：充分利用带宽，利用共享节点转发数据，建设一个远程连接的通用基础设施。

## 为什么选择OpenP2P
### 免费
完全免费，满足大部分用户的核心白票需求。不像其它类似的产品，我们不需要有公网IP的服务器，不需要花钱买服务。了解它原理即可理解为什么能做到免费。
### 安全
代码开源，接受各位大佬检验。下面详细展开
### 轻量
文件大小2MB+，运行内存2MB+；全部在应用层实现，没有虚拟网卡，没有内核程序
### 跨平台
因为轻量，所以很容易支持各个平台。支持主流的操作系统：Windows,Linux,MacOS；和主流的cpu架构：386、amd64、arm、arm64、mipsle、mipsle64、mips、mips64
### 高效
P2P直连可以让你的设备跑满带宽。不论你的设备在任何网络环境，无论NAT1-4（Cone或Symmetric），都支持。依靠Quic协议优秀的拥塞算法，能在糟糕的网络环境获得高带宽低延时。

### 二次开发
基于OpenP2P只需数行代码，就能让原来只能局域网通信的程序，变成任何内网都能通信

## 快速入门
以一个最常见的例子说明OpenP2P如何使用：远程办公，在家里连入办公室Windows电脑。  
相信很多人在疫情下远程办公是刚需。
1. 先确认办公室电脑已开启远程桌面功能（如何开启参考官方说明https://docs.microsoft.com/zh-cn/windows-server/remote/remote-desktop-services/clients/remote-desktop-allow-access）
2. 在办公室下载最新的[OpenP2P](https://gitee.com/tenderiron/openp2p/releases/),解压出来,在命令行执行
   ```
   openp2p.exe -d -node OFFICEPC1 -user USERNAME1 -password PASSWORD1  
   ```
   
   > :warning: **切记将标记大写的参数改成自己的**

   ![image](/doc/images/officelisten.png)
3. 在家里下载最新的[OpenP2P](https://gitee.com/tenderiron/openp2p/releases/),解压出来,在命令行执行
   ```
   openp2p.exe -d -node HOMEPC123 -user USERNAME1 -password PASSWORD1 --peernode OFFICEPC1 --dstip 127.0.0.1 --dstport 3389 --srcport 23389 --protocol tcp
   ```
   > :warning: **切记将标记大写的参数改成自己的**

   ![image](/doc/images/homeconnect.png)
   ![image](/doc/images/mem.png)
   `LISTEN ON PORT 23389 START` 看到这行日志表示P2PApp建立成功，监听23389端口。只需连接本机的127.0.0.1:23389就相当于连接公司Windows电脑的3389端口。
   
4. 在家里Windows电脑，按Win+R输入mstsc打开远程桌面，输入127.0.0.1:23389 /admin  
   ![image](/doc/images/mstscconnect.png)

   ![image](/doc/images/afterconnect.png)

## [详细使用说明](/USAGE-ZH.md)
## 典型应用场景
特别适合大流量的内网访问
### 远程办公
Windows MSTSC、VNC等远程桌面，SSH，内网各种ERP系统
### 远程访问NAS
管理大量视频、图片
### 远程监控摄像头
### 远程刷机
### 远程数据备份
---
## 概要设计
### 原型
![image](/doc/images/prototype.png)
### 客户端架构
![image](/doc/images/architecture.png)
### P2PApp
它是项目里最重要的概念，一个P2PApp就是把远程的一个服务（mstsc/ssh等）通过P2P网络映射到本地监听。二次开发或者我们提供的Restful API，主要工作就是管理P2PApp
![image](/doc/images/appdetail.png)
## 共享
默认会开启共享限速10mbps，只有你用户下提供了共享节点才能使用别人的共享节点。这非常公平，也是这个项目的初衷。
我们建议你在带宽足够的地方（比如办公室，家里的百兆光纤）加入共享网络。
如果你仍然不想共享任何节点，请查看运行参数
## 安全性
加入OpenP2P共享网络的节点，只能凭授权访问。共享节点只会中转数据，别人无法访问内网任何资源。
### TLS1.3+AES
两个节点间通信数据走业界最安全的TLS1.3通道。通信内容还会使用AES加密，双重安全，密钥是通过服务端作换。有效阻止中间人攻击
### 共享的中转节点是否会获得我的数据
没错，中转节点天然就是一个中间人，所以才加上AES加密通信内容保证安全。中转节点是无法获取明文的

### 中转节点是如何校验权限的
服务端有个调度模型，根据带宽、ping值、稳定性、服务时长，尽可能地使共享节点均匀地提供服务。连接共享节点使用TOTP密码，hmac-sha256算法校验，它是一次性密码，和我们平时使用的手机验证码或银行密码器一样的原理。

## 编译
cd到代码根目录，执行
```
export GOPROXY=https://goproxy.io,direct
go mod tidy
go build
```

## TODO
近期计划：
1. 支持IPv6
2. 支持随系统自动启动，安装成系统服务
3. 提供一些免费服务器给特别差的网络，如广电网络
4. 建立网站，用户可以在网站管理所有P2PApp和设备。查看设备在线状态，升级，增删查改重启P2PApp等
5. 建立公众号，用户可在微信公众号管理所有P2PApp和设备
6. 客户端提供WebUI
7. 支持自有服务器高并发连接
8. 共享节点调度模型优化，对不同的运营商优化
9. 方便二次开发，提供API和lib
10. 应用层支持UDP协议，实现很简单，但UDP应用较少暂不急
11. 底层通信支持KCP协议，目前仅支持Quic；KCP专门对延时优化，被游戏加速器广泛使用，可以牺牲一定的带宽降低延时
12. 支持Android系统，让旧手机焕发青春变成移动网关
13. 支持Windows网上邻居共享文件
14. 内网直连优化，用处不大，估计就用户测试时用到

远期计划：
1. 利用区块链技术去中心化，让共享设备的用户有收益，从而促进更多用户共享，达到正向闭环。
2. 企业级支持，可以更好地管理大量设备，和更安全更细的权限控制

## 参与贡献
TODO或ISSUE里如果有你擅长的领域，或者你有特别好的主意，可以加入OpenP2P项目，贡献你的代码。待项目茁壮成长后，你们就是知名开源项目的主要代码贡献者，岂不快哉。
## 商业合作
它是一个中国人发起的项目，更懂国内网络环境，更懂用户需求，更好的企业级支持
## 技术交流
QQ群：16947733  
邮箱：openp2p.cn@gmail.com 271357901@qq.com  

## 免责声明
本项目开源供大家学习和免费使用，禁止用于非法用途，任何不当使用本项目或意外造成的损失，本项目及相关人员不会承担任何责任。
