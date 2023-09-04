package openp2p

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func wrapTestContains(t *testing.T, iptree *IPTree, ip string, result bool) {
	if iptree.Contains(ip) == result {
		// t.Logf("compare version %s %s ok\n", v1, v2)
	} else {
		t.Errorf("test %s fail\n", ip)
	}
}
func wrapBenchmarkContains(t *testing.B, iptree *IPTree, ip string, result bool) {
	if iptree.Contains(ip) == result {
		// t.Logf("compare version %s %s ok\n", v1, v2)
	} else {
		t.Errorf("test %s fail\n", ip)
	}
}

func TestAllInputFormat(t *testing.T) {
	iptree := NewIPTree("219.137.185.70,127.0.0.1,127.0.0.0/8,192.168.1.0/24,192.168.3.100-192.168.3.255,192.168.100.0-192.168.200.255")
	wrapTestContains(t, iptree, "127.0.0.1", true)
	wrapTestContains(t, iptree, "127.0.0.2", true)
	wrapTestContains(t, iptree, "127.1.1.1", true)
	wrapTestContains(t, iptree, "219.137.185.70", true)
	wrapTestContains(t, iptree, "219.137.185.71", false)
	wrapTestContains(t, iptree, "192.168.1.2", true)
	wrapTestContains(t, iptree, "192.168.2.2", false)
	wrapTestContains(t, iptree, "192.168.3.1", false)
	wrapTestContains(t, iptree, "192.168.3.100", true)
	wrapTestContains(t, iptree, "192.168.3.255", true)
	wrapTestContains(t, iptree, "192.168.150.1", true)
	wrapTestContains(t, iptree, "192.168.250.1", false)
}

func TestSingleIP(t *testing.T) {
	iptree := NewIPTree("")
	iptree.Add("219.137.185.70", "219.137.185.70")
	wrapTestContains(t, iptree, "219.137.185.70", true)
	wrapTestContains(t, iptree, "219.137.185.71", false)
}

func TestWrongSegment(t *testing.T) {
	iptree := NewIPTree("")
	inserted := iptree.Add("87.251.75.0", "82.251.75.255")
	if inserted {
		t.Errorf("TestWrongSegment failed\n")
	}
}

func TestSegment2(t *testing.T) {
	iptree := NewIPTree("")
	iptree.Clear()
	iptree.Add("10.1.5.50", "10.1.5.100")
	iptree.Add("10.1.1.50", "10.1.1.100")
	iptree.Add("10.1.2.50", "10.1.2.100")
	iptree.Add("10.1.6.50", "10.1.6.100")
	iptree.Add("10.1.7.50", "10.1.7.100")
	iptree.Add("10.1.3.50", "10.1.3.100")
	iptree.Add("10.1.1.1", "10.1.1.10")    // no interset
	iptree.Add("10.1.1.200", "10.1.1.250") // no interset
	iptree.Print()

	iptree.Add("10.1.1.80", "10.1.1.90") // all in
	iptree.Add("10.1.1.40", "10.1.1.60") // interset
	iptree.Print()
	iptree.Add("10.1.1.90", "10.1.1.110") // interset
	iptree.Print()
	t.Logf("ipTree size:%d\n", iptree.Size())
	wrapTestContains(t, iptree, "10.1.1.40", true)
	wrapTestContains(t, iptree, "10.1.5.50", true)
	wrapTestContains(t, iptree, "10.1.6.50", true)
	wrapTestContains(t, iptree, "10.1.7.50", true)
	wrapTestContains(t, iptree, "10.1.2.50", true)
	wrapTestContains(t, iptree, "10.1.3.50", true)
	wrapTestContains(t, iptree, "10.1.1.60", true)
	wrapTestContains(t, iptree, "10.1.1.90", true)
	wrapTestContains(t, iptree, "10.1.1.110", true)
	wrapTestContains(t, iptree, "10.1.1.250", true)
	wrapTestContains(t, iptree, "10.1.2.60", true)
	wrapTestContains(t, iptree, "10.1.100.30", false)
	wrapTestContains(t, iptree, "10.1.200.30", false)

	iptree.Add("10.0.0.0", "10.255.255.255") // will merge all segment
	iptree.Print()
	if iptree.Size() != 1 {
		t.Errorf("merge ip segment error\n")
	}

}

func BenchmarkBuildipTree20k(t *testing.B) {
	iptree := NewIPTree("")
	iptree.Clear()
	iptree.Add("10.1.5.50", "10.1.5.100")
	iptree.Add("10.1.1.50", "10.1.1.100")
	iptree.Add("10.1.2.50", "10.1.2.100")
	iptree.Add("10.1.6.50", "10.1.6.100")
	iptree.Add("10.1.7.50", "10.1.7.100")
	iptree.Add("10.1.3.50", "10.1.3.100")
	iptree.Add("10.1.1.1", "10.1.1.10")    // no interset
	iptree.Add("10.1.1.200", "10.1.1.250") // no interset
	iptree.Add("10.1.1.80", "10.1.1.90")   // all in
	iptree.Add("10.1.1.40", "10.1.1.60")   // interset
	iptree.Add("10.1.1.90", "10.1.1.110")  // interset
	var minIP uint32
	binary.Read(bytes.NewBuffer(net.ParseIP("10.1.1.1").To4()), binary.BigEndian, &minIP)

	// insert 10k block ip single
	nodeNum := uint32(10000 * 1)
	gap := uint32(10)
	for i := minIP; i < minIP+nodeNum*gap; i += gap {
		iptree.AddIntIP(i, i)
		// t.Logf("ipTree size:%d\n", iptree.Size())
	}
	binary.Read(bytes.NewBuffer(net.ParseIP("100.1.1.1").To4()), binary.BigEndian, &minIP)
	// insert 100k block ip segment
	for i := minIP; i < minIP+nodeNum*gap; i += gap {
		iptree.AddIntIP(i, i+5)
	}
	t.Logf("ipTree size:%d\n", iptree.Size())
	iptree.Clear()
	t.Logf("clear. ipTree size:%d\n", iptree.Size())
}
func BenchmarkQuery(t *testing.B) {
	iptree := NewIPTree("")
	iptree.Clear()
	iptree.Add("10.1.5.50", "10.1.5.100")
	iptree.Add("10.1.1.50", "10.1.1.100")
	iptree.Add("10.1.2.50", "10.1.2.100")
	iptree.Add("10.1.6.50", "10.1.6.100")
	iptree.Add("10.1.7.50", "10.1.7.100")
	iptree.Add("10.1.3.50", "10.1.3.100")
	iptree.Add("10.1.1.1", "10.1.1.10")    // no interset
	iptree.Add("10.1.1.200", "10.1.1.250") // no interset
	iptree.Add("10.1.1.80", "10.1.1.90")   // all in
	iptree.Add("10.1.1.40", "10.1.1.60")   // interset
	iptree.Add("10.1.1.90", "10.1.1.110")  // interset
	var minIP uint32
	binary.Read(bytes.NewBuffer(net.ParseIP("10.1.1.1").To4()), binary.BigEndian, &minIP)

	// insert 10k block ip single
	nodeNum := uint32(10000 * 100)
	gap := uint32(10)
	for i := minIP; i < minIP+nodeNum*gap; i += gap {
		iptree.AddIntIP(i, i)
		// t.Logf("ipTree size:%d\n", iptree.Size())
	}
	binary.Read(bytes.NewBuffer(net.ParseIP("100.1.1.1").To4()), binary.BigEndian, &minIP)
	// insert 100k block ip segment
	for i := minIP; i < minIP+nodeNum*gap; i += gap {
		iptree.AddIntIP(i, i+5)
	}
	t.Logf("ipTree size:%d\n", iptree.Size())
	t.ResetTimer()
	queryNum := 100 * 10000
	for i := 0; i < queryNum; i++ {
		iptree.ContainsInt(minIP + uint32(i))
		wrapBenchmarkContains(t, iptree, "10.1.5.55", true)
		wrapBenchmarkContains(t, iptree, "10.1.1.1", true)
		wrapBenchmarkContains(t, iptree, "10.1.5.200", false)
		wrapBenchmarkContains(t, iptree, "200.1.1.1", false)
	}
	t.Logf("query list:%d\n", queryNum*4)

}
