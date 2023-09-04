#!/bin/sh


echo "Building version:${DOCKER_VER}"
echo "Running on platform: $TARGETPLATFORM"
# TARGETPLATFORM=$(echo $TARGETPLATFORM | tr ',' '/')
echo "Running on platform: $TARGETPLATFORM"
sysType="linux-amd64"
		archType=$(uname -m)
		if [[ $archType == aarch64 ]] ;
		then
		    sysType="linux-arm64"
		elif  [[ $archType == arm* ]] ;
		then
			sysType="linux-arm"
		elif  [[ $archType == i*86 ]] ;
		then
			sysType="linux-386"
		elif  [[ $archType == mips ]] ;
		then
			sysType="linux-mipsle"
			ls /lib |grep mipsel
			if [[ $? -ne 0 ]]; then
				# mipsel not found, it's mipseb
				sysType="linux-mipsbe"
			fi
		fi
url="https://openp2p.cn/download/v1/${DOCKER_VER}/openp2p-latest.$sysType.tar.gz"
echo "download $url start"

if [ -f /usr/bin/curl ]; then
	curl -k -o openp2p.tar.gz $url
else
  wget --no-check-certificate -O openp2p.tar.gz $url
fi
if [ $? -ne 0 ]; then
	echo "download error $?"
    exit 9
fi
echo "download ok"
tar -xzvf openp2p.tar.gz
chmod +x openp2p
pwd
ls -l
exit 0
