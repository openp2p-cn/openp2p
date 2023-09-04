package openp2p

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func update(host string, port int) error {
	gLog.Println(LvINFO, "update start")
	defer gLog.Println(LvINFO, "update end")
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		gLog.Println(LvERROR, "Failed to load system root CAs:", err)
	} else {
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AppendCertsFromPEM([]byte(rootCA))
	caCertPool.AppendCertsFromPEM([]byte(ISRGRootX1))

	c := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: caCertPool,
				InsecureSkipVerify: false},
		},
		Timeout: time.Second * 30,
	}
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	rsp, err := c.Get(fmt.Sprintf("https://%s:%d/api/v1/update?fromver=%s&os=%s&arch=%s&user=%s&node=%s", host, port, OpenP2PVersion, goos, goarch, gConf.Network.User, gConf.Network.Node))
	if err != nil {
		gLog.Println(LvERROR, "update:query update list failed:", err)
		return err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		gLog.Println(LvERROR, "get update info error:", rsp.Status)
		return err
	}
	rspBuf, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		gLog.Println(LvERROR, "update:read update list failed:", err)
		return err
	}
	updateInfo := UpdateInfo{}
	if err = json.Unmarshal(rspBuf, &updateInfo); err != nil {
		gLog.Println(LvERROR, rspBuf, " update info decode error:", err)
		return err
	}
	if updateInfo.Error != 0 {
		gLog.Println(LvERROR, "update error:", updateInfo.Error, updateInfo.ErrorDetail)
		return err
	}
	err = updateFile(updateInfo.Url, "", "openp2p")
	if err != nil {
		gLog.Println(LvERROR, "update: download failed:", err)
		return err
	}
	return nil
}

// todo rollback on error
func updateFile(url string, checksum string, dst string) error {
	gLog.Println(LvINFO, "download ", url)
	tmpFile := filepath.Dir(os.Args[0]) + "/openp2p.tmp"
	output, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0776)
	if err != nil {
		gLog.Printf(LvERROR, "OpenFile %s error:%s", tmpFile, err)
		return err
	}
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		gLog.Println(LvERROR, "Failed to load system root CAs:", err)
	} else {
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AppendCertsFromPEM([]byte(rootCA))
	caCertPool.AppendCertsFromPEM([]byte(ISRGRootX1))
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:            caCertPool,
			InsecureSkipVerify: false},
	}
	client := &http.Client{Transport: tr}
	response, err := client.Get(url)
	if err != nil {
		gLog.Printf(LvERROR, "download url %s error:%s", url, err)
		output.Close()
		return err
	}
	defer response.Body.Close()
	n, err := io.Copy(output, response.Body)
	if err != nil {
		gLog.Printf(LvERROR, "io.Copy error:%s", err)
		output.Close()
		return err
	}
	output.Sync()
	output.Close()
	gLog.Println(LvINFO, "download ", url, " ok")
	gLog.Printf(LvINFO, "size: %d bytes", n)

	err = os.Rename(os.Args[0], os.Args[0]+"0") // the old daemon process was using the 0 file, so it will prevent override it
	if err != nil {
		gLog.Printf(LvINFO, " rename %s error:%s, retry 1", os.Args[0], err)
		err = os.Rename(os.Args[0], os.Args[0]+"1")
		if err != nil {
			gLog.Printf(LvINFO, " rename %s error:%s", os.Args[0], err)
		}
	}
	// extract
	gLog.Println(LvINFO, "extract files")
	err = extract(filepath.Dir(os.Args[0]), tmpFile)
	if err != nil {
		gLog.Printf(LvERROR, "extract error:%s. revert rename", err)
		os.Rename(os.Args[0]+"0", os.Args[0])
		return err
	}
	os.Remove(tmpFile)
	return nil
}

func extract(dst, src string) (err error) {
	if runtime.GOOS == "windows" {
		return unzip(dst, src)
	} else {
		return extractTgz(dst, src)
	}
}

func unzip(dst, src string) (err error) {
	archive, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer archive.Close()

	for _, f := range archive.File {
		filePath := filepath.Join(dst, f.Name)
		fmt.Println("unzipping file ", filePath)
		if f.FileInfo().IsDir() {
			fmt.Println("creating directory...")
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return err
		}
		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		fileInArchive, err := f.Open()
		if err != nil {
			return err
		}
		if _, err := io.Copy(dstFile, fileInArchive); err != nil {
			return err
		}
		dstFile.Close()
		fileInArchive.Close()
	}
	return nil
}

func extractTgz(dst, src string) error {
	gzipStream, err := os.Open(src)
	if err != nil {
		return err
	}
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return err
	}
	tarReader := tar.NewReader(uncompressedStream)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(header.Name, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			filePath := filepath.Join(dst, header.Name)
			outFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if err != nil {
				return err
			}
			defer outFile.Close()
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

func cleanTempFiles() {
	err := os.Remove(os.Args[0] + "0")
	if err != nil {
		gLog.Printf(LvDEBUG, " remove %s error:%s", os.Args[0]+"0", err)
	}
	err = os.Remove(os.Args[0] + "1")
	if err != nil {
		gLog.Printf(LvDEBUG, " remove %s error:%s", os.Args[0]+"0", err)
	}
}
