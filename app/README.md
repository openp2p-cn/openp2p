## Build
```
cd core
go get -v golang.org/x/mobile/bind
gomobile bind -target android -v
if [[ $? -ne 0 ]]; then
    echo "build error"
    exit 9
fi
echo "build ok"
cp openp2p.aar openp2p-sources.jar ../app/app/libs
echo "copy to APP libs"

edit app/app/build.gradle 
```
signingConfigs {
        release {
            storeFile file('YOUR-JKS-PATH')
            storePassword 'YOUR-PASSWORD'
            keyAlias 'openp2p.keys'
            keyPassword 'YOUR-PASSWORD'
        }
    }
```
cd ../app
./gradlew build

```