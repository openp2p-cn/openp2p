package openp2p

import (
	"encoding/json"
	"testing"
)

func TestSetSDWAN_ChangeNode(t *testing.T) {
	conf := Config{}
	sdwanInfo := SDWANInfo{}
	sdwanStr := `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"},{"name":"tony-stable","ip":"10.2.3.4","resource":"10.1.0.0/16"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo)
	if len(conf.getDelNodes()) > 0 {
		t.Errorf("getDelNodes error")
		return
	}
	if len(conf.getAddNodes()) != 7 {
		t.Errorf("getAddNodes error")
		return
	}
	sdwanInfo2 := SDWANInfo{}
	sdwanStr = `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo2); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo2)
	diff := conf.getDelNodes()
	if len(diff) != 1 && diff[0].IP != "10.2.3.4" {
		t.Errorf("getDelNodes error")
		return
	}
	sdwanInfo3 := SDWANInfo{}
	sdwanStr = `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo3); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo3)
	diff = conf.getDelNodes()
	if len(diff) != 1 && diff[0].IP != "10.2.3.60" {
		t.Errorf("getDelNodes error")
		return
	}
	// add new node
	sdwanInfo4 := SDWANInfo{}
	sdwanStr = `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo4); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo4)
	diff = conf.getDelNodes()
	if len(diff) > 0 {
		t.Errorf("getDelNodes error")
		return
	}
	diff = conf.getAddNodes()
	if len(diff) != 1 && diff[0].IP != "10.2.3.60" {
		t.Errorf("getAddNodes error")
		return
	}
}

func TestSetSDWAN_ChangeNodeIP(t *testing.T) {
	conf := Config{}
	sdwanInfo := SDWANInfo{}
	sdwanStr := `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"},{"name":"tony-stable","ip":"10.2.3.4","resource":"10.1.0.0/16"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo)
	if len(conf.getDelNodes()) > 0 {
		t.Errorf("getDelNodes error")
		return
	}
	sdwanInfo2 := SDWANInfo{}
	sdwanStr = `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"},{"name":"tony-stable","ip":"10.2.3.44","resource":"10.1.0.0/16"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo2); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo2)
	diff := conf.getDelNodes()
	if len(diff) != 1 && diff[0].IP != "10.2.3.4" {
		t.Errorf("getDelNodes error")
		return
	}
	diff = conf.getAddNodes()
	if len(diff) != 1 || diff[0].IP != "10.2.3.44" {
		t.Errorf("getAddNodes error")
		return
	}
}
func TestSetSDWAN_ClearAll(t *testing.T) {
	conf := Config{}
	sdwanInfo := SDWANInfo{}
	sdwanStr := `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"},{"name":"tony-stable","ip":"10.2.3.4","resource":"10.1.0.0/16"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo)
	if len(conf.getDelNodes()) > 0 {
		t.Errorf("getDelNodes error")
		return
	}
	sdwanInfo2 := SDWANInfo{}
	sdwanStr = `{"Nodes":null}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo2); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo2)
	diff := conf.getDelNodes()
	if len(diff) != 7 {
		t.Errorf("getDelNodes error")
		return
	}
	diff = conf.getAddNodes()
	if len(diff) != 0 {
		t.Errorf("getAddNodes error")
		return
	}
}
func TestSetSDWAN_ChangeNodeResource(t *testing.T) {
	conf := Config{}
	sdwanInfo := SDWANInfo{}
	sdwanStr := `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"},{"name":"tony-stable","ip":"10.2.3.4","resource":"10.1.0.0/16"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo)
	if len(conf.getDelNodes()) > 0 {
		t.Errorf("getDelNodes error")
		return
	}
	sdwanInfo2 := SDWANInfo{}
	sdwanStr = `{"id":1312667996276071700,"name":"network1","gateway":"10.2.3.254/24","mode":"fullmesh","centralNode":"n1-stable","enable":1,"Nodes":[{"name":"222-debug","ip":"10.2.3.13"},{"name":"222stable","ip":"10.2.3.222"},{"name":"5800-debug","ip":"10.2.3.56"},{"name":"Mate60pro","ip":"10.2.3.60"},{"name":"Mymatepad2023","ip":"10.2.3.23"},{"name":"n1-stable","ip":"10.2.3.29","resource":"192.168.3.0/24"},{"name":"tony-stable","ip":"10.2.3.4","resource":"10.11.0.0/16"}]}`
	if err := json.Unmarshal([]byte(sdwanStr), &sdwanInfo2); err != nil {
		t.Errorf("unmarshal error")
		return
	}

	conf.setSDWAN(sdwanInfo2)
	diff := conf.getDelNodes()
	if len(diff) != 1 && diff[0].IP != "10.2.3.4" {
		t.Errorf("getDelNodes error")
		return
	}
	diff = conf.getAddNodes()
	if len(diff) != 1 || diff[0].Resource != "10.11.0.0/16" {
		t.Errorf("getAddNodes error")
		return
	}
}

func TestInetAtoN(t *testing.T) {
	ipa, _ := inetAtoN("121.5.147.4")
	t.Log(ipa)
	ipa, _ = inetAtoN("121.5.147.4/32")
	t.Log(ipa)
}
