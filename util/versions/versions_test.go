package versions

import (
	"testing"
)

func assertCompareVersion(t *testing.T, v1 string, v2 string, result int) {
	if Compare(v1, v2) != result {
		t.Errorf("compare version %s %s fail\n", v1, v2)
	}
}
func assertParseMajorVer(t *testing.T, v string, result int) {
	if ParseMajorVer(v) != result {
		t.Errorf("ParseMajorVer %s fail\n", v)
	}
}

func TestCompareVersion(t *testing.T) {
	// test =
	assertCompareVersion(t, "0.98.0", "0.98.0", EQUAL)
	assertCompareVersion(t, "0.98", "0.98", EQUAL)
	assertCompareVersion(t, "1.4.0", "1.4.0", EQUAL)
	assertCompareVersion(t, "1.5.0", "1.5.0", EQUAL)
	// test >
	assertCompareVersion(t, "0.98.0.22345", "0.98.0.12345", GREATER)
	assertCompareVersion(t, "1.98.0.12345", "0.98", GREATER)
	assertCompareVersion(t, "10.98.0.12345", "9.98.0.12345", GREATER)
	assertCompareVersion(t, "1.4.0", "0.98.0.12345", GREATER)
	assertCompareVersion(t, "1.4", "0.98.0.12345", GREATER)
	assertCompareVersion(t, "1", "0.98.0.12345", GREATER)
	// test <
	assertCompareVersion(t, "0.98.0.12345", "0.98.0.12346", LESS)
	assertCompareVersion(t, "9.98.0.12345", "10.98.0.12345", LESS)
	assertCompareVersion(t, "1.4.2", "1.5.0", LESS)
	assertCompareVersion(t, "", "1.5.0", LESS)

}
func TestParseMajorVer(t *testing.T) {

	assertParseMajorVer(t, "0.98.0", 0)
	assertParseMajorVer(t, "0.98", 0)
	assertParseMajorVer(t, "1.4.0", 1)
	assertParseMajorVer(t, "1.5.0", 1)

	assertParseMajorVer(t, "0.98.0.22345", 0)
	assertParseMajorVer(t, "1.98.0.12345", 1)
	assertParseMajorVer(t, "10.98.0.12345", 10)
	assertParseMajorVer(t, "1.4.0", 1)
	assertParseMajorVer(t, "1.4", 1)
	assertParseMajorVer(t, "1", 1)
	assertParseMajorVer(t, "2", 2)
	assertParseMajorVer(t, "3", 3)
	assertParseMajorVer(t, "2.1.0", 2)
	assertParseMajorVer(t, "3.0.0", 3)

}
