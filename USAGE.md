# Parameters details

> :warning: all commands in this doc, Windows env uses "openp2p.exe", Linux env uses "./openp2p" 


## Install and Listen
```
./openp2p install -node OFFICEPC1 -user USERNAME1 -password PASSWORD1  
Or
./openp2p -d -node OFFICEPC1 -user USERNAME1 -password PASSWORD1 

```
>* install: [recommand] will install as system service. So it will autorun when system booting.
>* -d: daemon mode run once. When the worker process is found to exit unexpectedly, a new worker process will be automatically started
>* -node: Unique node name, unique identification
>* -user: Unique user name, the node belongs to this user
>* -password: Password
>* -sharebandwidth: Provides bandwidth when used as a shared node, the default is 10mbps. If it is a large bandwidth of optical fiber, the larger the setting, the better the effect. -1 means not shared, the node is only used in a private P2P network. Do not join the shared P2P network, which also means that you CAN NOT use other people’s shared nodes
>* -loglevel: Need to view more debug logs, set 0; the default is 1

## Connect
```
./openp2p -d -node HOMEPC123 -user USERNAME1 -password PASSWORD1 -appname OfficeWindowsRemote -peernode OFFICEPC1 -dstip 127.0.0.1 -dstport 3389 -srcport 23389 -protocol tcp
Create multiple P2PApp by config file
./openp2p -d -f    
./openp2p -f 
```
>* -appname: This P2PApp name
>* -peernode: Target node name
>* -dstip: Target service address, default local 127.0.0.1
>* -dstport: Target service port, such as windows remote desktop 3389, Linux ssh 22
>* -protocol: Target service protocol tcp, udp
>* -peeruser: The target user, if it is a node under the same user, no need to set
>* -peerpassword: The target password, if it is a node under the same user, no need to set

## Config file
Generally saved in the current directory, in installation mode it will be saved to `C:\Program Files\OpenP2P\config.json` or `/usr/local/openp2p/config.json`
If you want to modify the parameters, or configure multiple P2PApps, you can manually modify the configuration file

Configuration example
```
{
  "network": {
    "Node": "hhd1207-222",
    "User": "USERNAME1",
    "Password": "PASSWORD1",
    "ShareBandwidth": -1,
    "ServerHost": "api.openp2p.cn",
    "ServerPort": 27182,
    "UDPPort1": 27182,
    "UDPPort2": 27183
  },
  "apps": [
    {
      "AppName": "OfficeWindowsPC",
      "Protocol": "tcp",
      "SrcPort": 23389,
      "PeerNode": "OFFICEPC1",
      "DstPort": 3389,
      "DstHost": "localhost",
      "PeerUser": "",
      "PeerPassword": ""
    },
    {
      "AppName": "OfficeServerSSH",
      "Protocol": "tcp",
      "SrcPort": 22,
      "PeerNode": "OFFICEPC1",
      "DstPort": 22,
      "DstHost": "192.168.1.5",
      "PeerUser": "",
      "PeerPassword": ""
    }
  ]
}
```
## Client update
```
# update local client
./openp2p update  
# update remote client
curl --insecure 'https://openp2p.cn:27182/api/v1/device/YOUR-NODE-NAME/update?user=&password='
```

Windows system needs to set up firewall for this program, the program will automatically set the firewall, if the setting fails, the UDP punching will be affected.  
The default firewall configuration of Linux system (Ubuntu and CentOS7) will not have any effect, if not, you can try to turn off the firewall
```
systemctl stop firewalld.service
systemctl start firewalld.service
firewall-cmd --state
```

## Uninstall
```
./openp2p uninstall
```