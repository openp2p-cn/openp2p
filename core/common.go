package openp2p

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const MinNodeNameLen = 8

func getmac(ip string) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	firstMac := ""
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if firstMac == "" {
				firstMac = iface.HardwareAddr.String()
			}
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.String() == ip {
				if iface.HardwareAddr.String() != "" {
					return iface.HardwareAddr.String()
				}
				return firstMac
			}
		}
	}
	return firstMac
}

var cbcIVBlock = []byte("UHNJUSBACIJFYSQN")

var paddingArray = [][]byte{
	{0},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2},
	{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4},
	{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6},
	{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7},
	{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8},
	{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
	{10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10},
	{11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11},
	{12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12},
	{13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13},
	{14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14},
	{15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15},
	{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16},
}

func pkcs7Padding(plainData []byte, dataLen, blockSize int) int {
	padLen := blockSize - dataLen%blockSize
	pPadding := plainData[dataLen : dataLen+padLen]

	copy(pPadding, paddingArray[padLen][:padLen])
	return padLen
}

func pkcs7UnPadding(origData []byte, dataLen int) ([]byte, error) {
	unPadLen := int(origData[dataLen-1])
	if unPadLen <= 0 || unPadLen > 16 {
		return nil, fmt.Errorf("wrong pkcs7 padding head size:%d", unPadLen)
	}
	return origData[:(dataLen - unPadLen)], nil
}

// AES-CBC
func encryptBytes(key []byte, out, in []byte, plainLen int) ([]byte, error) {
	if len(key) == 0 {
		return in[:plainLen], nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	//iv := out[:aes.BlockSize]
	//if _, err := io.ReadFull(rand.Reader, iv); err != nil {
	//	return nil, err
	//}
	mode := cipher.NewCBCEncrypter(block, cbcIVBlock)
	total := pkcs7Padding(in, plainLen, aes.BlockSize) + plainLen
	mode.CryptBlocks(out[:total], in[:total])
	return out[:total], nil
}

func decryptBytes(key []byte, out, in []byte, dataLen int) ([]byte, error) {
	if len(key) == 0 {
		return in[:dataLen], nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := cipher.NewCBCDecrypter(block, cbcIVBlock)
	mode.CryptBlocks(out[:dataLen], in[:dataLen])
	return pkcs7UnPadding(out, dataLen)
}

// {240e:3b7:622:3440:59ad:7fa1:170c:ef7f 47924975352157270363627191692449083263 China CN 0xc0000965c8 Guangdong GD 0  Guangzhou 23.1167 113.25 Asia/Shanghai AS4134 Chinanet }
func netInfo() *NetInfo {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		// DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		// 	var d net.Dialer
		// 	return d.DialContext(ctx, "tcp6", addr)
		// },
	}
	// sometime will be failed, retry
	for i := 0; i < 2; i++ {
		client := &http.Client{Transport: tr, Timeout: time.Second * 10}
		r, err := client.Get("https://ifconfig.co/json")
		if err != nil {
			gLog.d("netInfo error:%s", err)
			continue
		}
		defer r.Body.Close()
		buf := make([]byte, 1024*64)
		n, err := r.Body.Read(buf)
		if err != nil {
			gLog.d("netInfo error:%s", err)
			continue
		}
		rsp := NetInfo{}
		if err = json.Unmarshal(buf[:n], &rsp); err != nil {
			gLog.e("wrong NetInfo:%s", err)
			continue
		}
		return &rsp
	}
	return nil
}

func execOutput(name string, args ...string) string {
	cmdGetOsName := exec.Command(name, args...)
	var cmdOut bytes.Buffer
	cmdGetOsName.Stdout = &cmdOut
	cmdGetOsName.Run()
	return cmdOut.String()
}

func defaultNodeName() string {
	name, _ := os.Hostname()
	for len(name) < MinNodeNameLen {
		name = fmt.Sprintf("%s%d", name, rand.Int()%10)
	}
	return name
}

const EQUAL int = 0
const GREATER int = 1
const LESS int = -1

func compareVersion(v1, v2 string) int {
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

func parseMajorVer(ver string) int {
	v1Arr := strings.Split(ver, ".")
	if len(v1Arr) > 0 {
		n, _ := strconv.ParseInt(v1Arr[0], 10, 32)
		return int(n)
	}
	return 0
}

func IsIPv6(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.To16() != nil && ip.To4() == nil
}

var letters = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890-")

func randStr(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func execCommand(commandPath string, wait bool, arg ...string) (err error) {
	command := exec.Command(commandPath, arg...)
	err = command.Start()
	if err != nil {
		return
	}
	if wait {
		err = command.Wait()
	}
	return
}

func sanitizeFileName(fileName string) string {
	validFileName := fileName
	invalidChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range invalidChars {
		validFileName = strings.ReplaceAll(validFileName, char, " ")
	}
	return validFileName
}

func prettyJson(s interface{}) string {
	jsonData, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fmt.Println("Error marshalling JSON:", err)
		return ""
	}
	return string(jsonData)
}

func inetAtoN(ipstr string) (uint32, error) { // support both ipnet or single ip
	i, _, err := net.ParseCIDR(ipstr)
	if err != nil {
		i = net.ParseIP(ipstr)
		if i == nil {
			return 0, err
		}
	}
	ret := big.NewInt(0)
	ret.SetBytes(i.To4())
	return uint32(ret.Int64()), nil
}

func calculateChecksum(data []byte) uint16 {
	length := len(data)
	sum := uint32(0)

	// Calculate the sum of 16-bit words
	for i := 0; i < length-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}

	// Add the last byte (if odd length)
	if length%2 != 0 {
		sum += uint32(data[length-1])
	}

	// Fold 32-bit sum to 16 bits
	sum = (sum >> 16) + (sum & 0xffff)
	sum += (sum >> 16)

	return uint16(^sum)
}

func min(nums ...int32) int32 {
	if len(nums) == 0 {
		return 0 // 如果没有输入，返回最大值
	}

	minVal := nums[0]
	for _, num := range nums[1:] {
		if num < minVal {
			minVal = num
		}
	}
	return minVal
}

func calcRetryTimeRelay(x float64) float64 {
	return 10 + math.Exp(0.8*(x-3.6))
}
func calcRetryTimeDirect(x float64) float64 {
	return 10 + math.Exp(2.8*(x-4))
}

func isAndroid() bool {
	if runtime.GOOS == "android" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "Android")
}

func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	// windows could not rename running executable, so copy then delete
	if runtime.GOOS == "windows" {
		err = copyFile(src, dst)
		if err != nil {
			return err
		}

		os.Remove(src)
	}
	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}

func resolveServerIP(host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 先系统 DNS
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err == nil && len(ips) > 0 {
		gLog.i("system dns resolved %s -> %v", host, ips)
		return ips, nil
	}

	gLog.e("system dns resolve failed for %s: %v", host, err)
	gLog.i("retry with fallback dns...")

	// 再 fallback dns
	return lookupWithCustomDNS(ctx, host)
}
func lookupWithCustomDNS(ctx context.Context, domain string) ([]string, error) {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 5 * time.Second}

			// 先 119.29.29.29
			conn, err := dialer.DialContext(ctx, network, "119.29.29.29:53")
			if err == nil {
				return conn, nil
			}

			// 再 8.8.8.8
			return dialer.DialContext(ctx, network, "8.8.8.8:53")
		},
	}

	return resolver.LookupHost(ctx, domain)
}
