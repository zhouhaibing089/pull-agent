package proxy

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/zhouhaibing089/pull-agent/pkg/cluster"
)

// Proxy is a simple
type Proxy struct {
	cluster  cluster.Interface
	port     int64
	server   *http.Server
	localDir string
}

// New creates a proxy server which is used for downloading files.
func New(addr string, port int64, localDir string, cluster cluster.Interface) *Proxy {
	addr = fmt.Sprintf("%s:%d", addr, port)
	return &Proxy{
		server: &http.Server{
			Addr: addr,
		},
		port:     port,
		cluster:  cluster,
		localDir: localDir,
	}
}

// ListenAndServe runs the http server.
func (p *Proxy) ListenAndServe() error {
	p.server.Handler = http.HandlerFunc(p.HandlerFunc)
	return p.server.ListenAndServe()
}

// HandlerFunc is the http handler.
func (p *Proxy) HandlerFunc(writer http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	// len is a required parameter.
	qsLen := req.URL.Query().Get("len")
	length, err := strconv.ParseInt(qsLen, 10, 64)
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Length", qsLen)

	// if relay is specified.
	relay := req.URL.Query().Get("relay")
	if relay != "" {
		p.copyFromFile(writer, path, length)
		return
	}

	// see whether there is currently a node is downloading this layer
	nodes := p.cluster.Endpoints(path)
	if len(nodes) >= 1 {
		// select one randomly to balance load
		node := nodes[rand.Intn(len(nodes))]
		url := fmt.Sprintf("http://%s:%d%s?relay=true&len=%d", node, p.port, path, length)
		p.copyFromURL(writer, path, url)
		return
	}

	// download directly from source
	source := req.URL.Query().Get("source")
	if source != "" {
		source, err := url.QueryUnescape(source)
		if err != nil {
			log.Printf("failed to unescape source query param: %s", err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		p.copyFromURL(writer, path, source)
		return
	}
}

// fileWriter wraps the standard ResponseWriter with extra functionality to write
// files at the same time.
type fileWriter struct {
	File   *os.File
	Writer io.Writer
}

// Write implements the io.Writer interface.
func (fw *fileWriter) Write(data []byte) (int, error) {
	fw.File.Write(data)
	return fw.Writer.Write(data)
}

// copyFromFile copies the data from file to writer.
func (p *Proxy) copyFromFile(writer http.ResponseWriter, path string, length int64) {
	log.Printf("copy from file for %s", path)

	filePath := fmt.Sprintf("%s%s", p.localDir, path)
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("failed to open file %q: %s", filePath, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer file.Close()
	// copy the file to writer
	var offset int64
	var buffer = make([]byte, 50*1024*1024)
	for offset != (length - 1) {
		n, err := file.ReadAt(buffer, offset)
		if err != nil && err == io.EOF {
			if offset < length {
				time.Sleep(300 * time.Millisecond)
			}
		} else if err != nil {
			log.Printf("failed to read from file: %s", err)
		}
		n, err = writer.Write(buffer[0:n])
		if err != nil {
			log.Printf("failed to write: %s", err)
		}
		offset += int64(n)
	}
}

// copyFromURL copies the data from remote server to writer.
func (p *Proxy) copyFromURL(writer http.ResponseWriter, path string, url string) {
	log.Printf("copy from url %q for %s", url, path)

	// send the http request to url
	resp, err := http.DefaultClient.Get(url)
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	defer resp.Body.Close()
	filePath := fmt.Sprintf("%s%s", p.localDir, path)
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("failed to create file %q: %s", filePath, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	// announce to cluster that we are downloading this layer.
	p.cluster.StartLayer(path, 0)
	defer p.cluster.EndLayer(path)

	fw := &fileWriter{
		File:   file,
		Writer: writer,
	}
	// make a buffer size of 5MB
	_, err = io.CopyBuffer(fw, resp.Body, make([]byte, 100*1024*1024))
	// if anything goes wrong, close and remove the file.
	if err != nil {
		log.Printf("failed to copy from url %q: %s", url, err)
		// close and remove this file
		fw.File.Close()
		os.Remove(filePath)
	}
}
