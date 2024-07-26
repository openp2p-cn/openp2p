package main

// On Windows env
// cd lib
// go build -o openp2p.dll -buildmode=c-shared openp2p.go
// caller example see example/dll
import (
	op "openp2p/core"
)
import "C"

func main() {
}

//export RunCmd
func RunCmd(cmd *C.char) {
	op.RunCmd(C.GoString(cmd))
}
