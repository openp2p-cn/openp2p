package openp2p

import (
	"log"
	"os/exec"
	"runtime"
)

func allowTunForward() {
	if runtime.GOOS != "linux" { // only support Linux
		return
	}
	exec.Command("sh", "-c", `iptables -t filter -D FORWARD -i optun -j ACCEPT`).Run()
	exec.Command("sh", "-c", `iptables -t filter -D FORWARD -o optun -j ACCEPT`).Run()
	err := exec.Command("sh", "-c", `iptables -t filter -I FORWARD -i optun -j ACCEPT`).Run()
	if err != nil {
		log.Println("allow foward in error:", err)
	}
	err = exec.Command("sh", "-c", `iptables -t filter -I FORWARD -o optun -j ACCEPT`).Run()
	if err != nil {
		log.Println("allow foward out error:", err)
	}
}

func clearSNATRule() {
	if runtime.GOOS != "linux" {
		return
	}
	execCommand("iptables", true, "-t", "nat", "-D", "POSTROUTING", "-j", "OPSDWAN")
	execCommand("iptables", true, "-t", "nat", "-F", "OPSDWAN")
	execCommand("iptables", true, "-t", "nat", "-X", "OPSDWAN")
}

func initSNATRule(localNet string) {
	if runtime.GOOS != "linux" {
		return
	}
	clearSNATRule()

	err := execCommand("iptables", true, "-t", "nat", "-N", "OPSDWAN")
	if err != nil {
		log.Println("iptables new sdwan chain error:", err)
		return
	}
	err = execCommand("iptables", true, "-t", "nat", "-A", "POSTROUTING", "-j", "OPSDWAN")
	if err != nil {
		log.Println("iptables append postrouting error:", err)
		return
	}
	err = execCommand("iptables", true, "-t", "nat", "-A", "OPSDWAN",
		"-o", "optun", "!", "-s", localNet, "-j", "MASQUERADE")
	if err != nil {
		log.Println("add optun snat error:", err)
		return
	}
	err = execCommand("iptables", true, "-t", "nat", "-A", "OPSDWAN", "!", "-o", "optun",
		"-s", localNet, "-j", "MASQUERADE")
	if err != nil {
		log.Println("add optun snat error:", err)
		return
	}
}

func addSNATRule(target string) {
	if runtime.GOOS != "linux" {
		return
	}
	err := execCommand("iptables", true, "-t", "nat", "-A", "OPSDWAN", "!", "-o", "optun",
		"-s", target, "-j", "MASQUERADE")
	if err != nil {
		log.Println("iptables add optun snat error:", err)
		return
	}
}
