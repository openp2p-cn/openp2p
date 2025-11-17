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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func update(host string, port int) error {
	gLog.i("update start")
	defer gLog.i("update end")
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		gLog.e("Failed to load system root CAs:%s", err)
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AppendCertsFromPEM([]byte(rootCA))
	caCertPool.AppendCertsFromPEM([]byte(rootEdgeCA))
	caCertPool.AppendCertsFromPEM([]byte(ISRGRootX1))

	c := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: caCertPool,
				InsecureSkipVerify: gConf.TLSInsecureSkipVerify},
		},
		Timeout: time.Second * 30,
	}
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	rsp, err := c.Get(fmt.Sprintf("https://%s:%d/api/v1/update?fromver=%s&os=%s&arch=%s&user=%s&node=%s", host, port, OpenP2PVersion, goos, goarch, url.QueryEscape(gConf.Network.User), url.QueryEscape(gConf.Network.Node)))
	if err != nil {
		gLog.e("update:query update list failed:%s", err)
		return err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		gLog.e("get update info error:%s", rsp.Status)
		return err
	}
	rspBuf, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		gLog.e("update:read update list failed:%s", err)
		return err
	}
	updateInfo := UpdateInfo{}
	if err = json.Unmarshal(rspBuf, &updateInfo); err != nil {
		gLog.e("%s update info decode error:%s", string(rspBuf), err)
		return err
	}
	if updateInfo.Error != 0 {
		gLog.e("update error:%d,%s", updateInfo.Error, updateInfo.ErrorDetail)
		return err
	}
	err = updateFile(updateInfo.Url, "", "openp2p")
	if err != nil {
		gLog.e("update: download failed:%s, retry...", err)
		err = updateFile(updateInfo.Url2, "", "openp2p")
		if err != nil {
			gLog.e("update: download failed:%s", err)
			return err
		}
	}
	return nil
}

func downloadFile(url string, checksum string, dstFile string) error {
	output, err := os.OpenFile(dstFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0776)
	if err != nil {
		gLog.e("OpenFile %s error:%s", dstFile, err)
		return err
	}
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		gLog.e("Failed to load system root CAs:%s", err)
		caCertPool = x509.NewCertPool()
	}
	caCertPool.AppendCertsFromPEM([]byte(rootCA))
	caCertPool.AppendCertsFromPEM([]byte(rootEdgeCA))
	caCertPool.AppendCertsFromPEM([]byte(ISRGRootX1))
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:            caCertPool,
			InsecureSkipVerify: gConf.TLSInsecureSkipVerify},
	}
	client := &http.Client{Transport: tr}
	response, err := client.Get(url)
	if err != nil {
		gLog.e("download url %s error:%s", url, err)
		output.Close()
		return err
	}
	defer response.Body.Close()
	n, err := io.Copy(output, response.Body)
	if err != nil {
		gLog.e("io.Copy error:%s", err)
		output.Close()
		return err
	}
	output.Sync()
	output.Close()
	gLog.i("download %s ok", url)
	gLog.i("size: %d bytes", n)
	return nil
}

func updateFile(url string, checksum string, dst string) error {
	gLog.i("download %s", url)
	tempDir := os.TempDir()
	tmpFile := filepath.Join(tempDir, "openp2p.tmp")
	err := downloadFile(url, checksum, tmpFile)
	if err != nil {
		return err
	}
	backupBase := filepath.Base(os.Args[0])
	var backupFile string
	if runtime.GOOS == "windows" {
		backupFile = filepath.Join(tempDir, backupBase+"0")
	} else {
		backupFile = os.Args[0] + "0" // linux can not mv running executable to /tmp, because they are different volumns
	}
	gLog.i("backup file %s --> %s", os.Args[0], backupFile)
	err = moveFile(os.Args[0], backupFile)
	if err != nil {
		if runtime.GOOS == "windows" {
			backupFile = filepath.Join(tempDir, backupBase+"1")
		} else {
			backupFile = os.Args[0] + "1" // 1st update will mv deamon process to 0, 2nd update mv to 0 will failed, mv to 1
		}
		gLog.i("backup file %s --> %s", os.Args[0], backupFile)
		err = moveFile(os.Args[0], backupFile)
		if err != nil {
			gLog.e(" rename %s error:%s", os.Args[0], err)
			return err
		}
	}
	// extract
	gLog.i("extract files")
	err = extract(filepath.Dir(os.Args[0]), tmpFile)
	if err != nil {
		gLog.e("extract error:%s. revert rename", err)
		moveFile(backupFile, os.Args[0])
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
	tempDir := os.TempDir()
	backupBase := filepath.Base(os.Args[0])
	for i := 0; i < 2; i++ {
		tmpFile := fmt.Sprintf("%s%d", os.Args[0], i)
		if _, err := os.Stat(tmpFile); err == nil {
			if err := os.Remove(tmpFile); err != nil {
				gLog.d(" remove %s error:%s", tmpFile, err)
			}
		}
		tmpFile = fmt.Sprintf("%s%s%d", tempDir, backupBase, i)
		if _, err := os.Stat(tmpFile); err == nil {
			if err := os.Remove(tmpFile); err != nil {
				gLog.d(" remove %s error:%s", tmpFile, err)
			}
		}
	}
}
