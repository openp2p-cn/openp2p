# 1. 环境
golang1.18.1
调试端: win10+vscode
被调试端: ubuntu20.04LTS+dlv
## dlv安装
参考官方文档https://github.com/go-delve/delve/tree/master/Documentation/installation
```
go install github.com/go-delve/delve/cmd/dlv@latest
```

## 被调试端
```
cd /your-src-path #要确保两端源码一致
dlv debug --headless --listen=:2345 --api-version=2
# 如果失败可查看更多日志
# dlv debug --headless --listen=:2345 --api-version=2 --log --log-output=rpc,dap,debugger
```

## 调试端（vscode）
```
打开vscode，修改launch.json，增加下面远程调试配置
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "RemoteDebug",
            "type": "go",
            "request": "attach",
            "mode": "remote",
            "port": 2345,
            "host": "192.168.3.29",
        }
    ]
}
按F5启动调试
```

## 没有公网IP或不在同一个局域网，无法直连如何调试
到https://openp2p.cn/注册一个用户获得token，两端安装一个客户端程序，可将被调试端的2345端口，通过p2p连接映射到调试端本地。
p2p连接可通过web配置 https://github.com/openp2p-cn/openp2p/blob/master/README-ZH.md#%E5%BF%AB%E9%80%9F%E5%85%A5%E9%97%A8

也可以手动下载https://github.com/openp2p-cn/openp2p/releases 通过命令行手动配置

```
# 注意替换下面YOUR-开头的参数改成自己的
./openp2p -node YOUR-DEBUG-SERVER -token YOUR-TOKEN

openp2p.exe -node YOUR-DEBUG-CLIENT -token YOUR-TOKEN -peernode YOUR-DEBUG-SERVER -dstport 2345 -srcport 2345

2022/04/22 11:07:26 25680 INFO LISTEN ON PORT tcp:2345 START 
#显示这条日志说明成功了
```

p2p端口映射完成后，把vscode的配置改成本地127.0.0.1，一样可以顺利调试。
```
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "RemoteDebug",
            "type": "go",
            "request": "attach",
            "mode": "remote",
            "port": 2345,
            "host": "127.0.0.1",
        }
    ]
}
```


# 参考
https://github.com/golang/vscode-go/blob/master/docs/debugging.md#remote-debugging