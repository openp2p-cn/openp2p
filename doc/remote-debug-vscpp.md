# 1. 环境
visual studio 2022
调试端: win10
被调试端: win10
## 程序编译
一般远程调试我会选择release版本，然后将优化去掉即可，这样更接近真实版本。
![image](/doc/images/release-debug.png)

```
go install github.com/go-delve/delve/cmd/dlv@latest
```

## 运行远程调试器
C:\Program Files\Microsoft Visual Studio\2022\Community\Common7\IDE\Remote Debugger 拷贝到目标机器

如果调试x64程序则cd到Remote Debugger\x64目录

管理员方式打开cmd，执行
```
 netsh advfirewall set allprofiles state off
 msvsmon.exe /noauth /anyuser /silent
```

## visual studio

Attach远程进程，按下Ctrl+Atl+P

![image](/doc/images/vs2022-remote-debug-attach.png)


## 没有公网IP或不在同一个局域网，无法直连如何调试
到 https://openp2p.cn/ 注册一个用户获得token，两端安装一个客户端程序，可将被调试端的2345端口，通过p2p连接映射到调试端本地。
p2p连接可通过web配置 https://github.com/openp2p-cn/openp2p/blob/master/README-ZH.md#%E5%BF%AB%E9%80%9F%E5%85%A5%E9%97%A8

也可以手动下载https://github.com/openp2p-cn/openp2p/releases 通过命令行手动配置

```
# 注意替换下面YOUR-开头的参数改成自己的
./openp2p -node YOUR-DEBUG-SERVER -token YOUR-TOKEN

openp2p.exe -node YOUR-DEBUG-CLIENT -token YOUR-TOKEN -peernode YOUR-DEBUG-SERVER -dstport 4026 -srcport 4026

2022/04/22 11:07:26 25680 INFO LISTEN ON PORT tcp:4026 START 
#显示这条日志说明成功了
```

![image](/doc/images/p2p-debug.png)

可以顺利远程调试
```