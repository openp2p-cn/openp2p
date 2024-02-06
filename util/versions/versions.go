package versions

import (
	"strconv"
	"strings"
)

const EQUAL int = 0
const GREATER int = 1
const LESS int = -1

func Compare(v1, v2 string) int {
	if v1 == v2 {
		return EQUAL
	}
	v1Arr := strings.Split(v1, ".")
	v2Arr := strings.Split(v2, ".")
	for i, subVer := range v1Arr {
		if len(v2Arr) <= i {
			return GREATER
		}
		subv1, _ := strconv.Atoi(subVer)
		subv2, _ := strconv.Atoi(v2Arr[i])
		if subv1 > subv2 {
			return GREATER
		}
		if subv1 < subv2 {
			return LESS
		}
	}
	return LESS
}

// unused?
func ParseMajorVer(ver string) int {
	v1Arr := strings.Split(ver, ".")
	if len(v1Arr) > 0 {
		n, _ := strconv.ParseInt(v1Arr[0], 10, 32)
		return int(n)
	}
	return 0
}
