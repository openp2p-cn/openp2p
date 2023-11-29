if not defined GOPROXY set GOPROXY=https://goproxy.io,direct
go env
@pause
go mod tidy
@pause
go build cmd/openp2p.go