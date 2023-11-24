# example: make PUTENV=export
build:
ifndef PUTENV
	@echo *** error: PUTENV not defined ***
endif
	$(PUTENV) GOPROXY=https://goproxy.io,direct
	go mod tidy
	go build cmd/openp2p.go
.PHONY: build

.DEFAULT_GOAL := build
