package main

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
