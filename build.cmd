@echo.
@set GOPROXY
if not defined GOPROXY set GOPROXY=https://goproxy.io,direct
go mod tidy
if ERRORLEVEL 1 pause (value:%ERRORLEVEL%)
go build cmd/openp2p.go
@echo.
@pause
