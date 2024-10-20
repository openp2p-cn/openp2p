## Build
depends on openjdk 11, gradle 8.1.3, ndk 21
```

# latest version not support go1.20
go install golang.org/x/mobile/cmd/gomobile@7c4916698cc93475ebfea76748ee0faba2deb2a5
gomobile init
go get -v golang.org/x/mobile/bind@7c4916698cc93475ebfea76748ee0faba2deb2a5
cd core
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