package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"
)

//Build executable without cmd window
//go build -ldflags -H=windowsgui
// to reduce binary size:
//go build -ldflags="-s -w"

var port string
var root string
var uploadDir string
// var showIp bool

func init() {

	flag.StringVar(&port, "p", "5000", "port to listen")
	flag.StringVar(&root, "root", "static", "file system path")
	flag.StringVar(&uploadDir, "ud", ".", "where to store the uploads")
	flag.Parse()
}

func getOutboundIP() net.IP {
	conn, err := net.Dial("udp", "10.254.254.254:1")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
}

type MyHandlers struct {
	Root string
}

func ping(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RemoteAddr, r.Method, r.URL.Path)
	switch r.Method {
    case http.MethodGet:
        // Handle GET request
        w.WriteHeader(http.StatusOK)
        fmt.Fprintln(w, "pong")
        
    case http.MethodPost:
        // Handle POST request
        body, err := io.ReadAll(r.Body)
        if err != nil {
            http.Error(w, "Failed to read request body", http.StatusInternalServerError)
            return
        }
        defer r.Body.Close() // Ensure the body is closed after reading

        // Log the posted text
        log.Printf("Received POST data: %s", body)

        // Respond to the client
        w.WriteHeader(http.StatusOK)
        fmt.Fprintln(w, "Done")

    default:
        // Handle unsupported methods
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
}

// func (params *MyHandlers) download(w http.ResponseWriter, r *http.Request) {
// 	log.Println(r.RemoteAddr, r.Method, r.URL.Path)
// 	srcFile := filepath.Join(UPLOADS, r.URL.Path)
// 	sourceFileStat, err := os.Stat(srcFile)
// 	if err != nil {
// 		http.Error(w, "Processing error", http.StatusInternalServerError)
// 		log.Print(err.Error())
// 		return
// 	}
// 	source, err := os.Open(srcFile)
// 	if err != nil {
// 		http.Error(w, "Error opening file", http.StatusInternalServerError)
// 		log.Print(err.Error())
// 		return
// 	}
// 	defer source.Close()
// 	w.Header().Set("Content-Disposition", "attachment; filename="+ sourceFileStat.Name())
// 	// w.Header().Set("Content-Type", )
// 	w.Header().Set("Content-Length", fmt.Sprintf("%d",sourceFileStat.Size()))

// 	//stream the body to the client without fully loading it into memory
// 	io.Copy(w, source)
// }

func serve(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RemoteAddr, r.Method, r.URL.Path[1:])
    http.ServeFile(w, r, r.URL.Path[1:])
}

func (params *MyHandlers) uploader(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RemoteAddr, r.Method, r.URL.Path)
	if r.Method == "GET" {
		t, _ := template.ParseFiles(filepath.Join(params.Root, "upload.gtpl"))
		t.Execute(w, nil)
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Processing error", http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}
	fileCount := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			log.Println("Reached EOF")
			break
		}
		//if FileName is empty, skip this iteration.
		if part.FileName() == "" {
			log.Println("FileName is empty")
			continue
		}
		fullFileName := filepath.Join(uploadDir, part.FileName())
		dst, err := os.Create(fullFileName)
		if err != nil {
			http.Error(w, "Processing error", http.StatusInternalServerError)
			log.Print(err.Error())
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, part); err != nil {
			http.Error(w, "Unable to Upload", http.StatusInternalServerError)
			log.Print(err.Error())
			return
		}
		fileCount++
		log.Printf("Saved %d %s", fileCount, fullFileName)
	}
	fmt.Fprintf(w, "Uploaded %d file(s)", fileCount)
	log.Printf("Saved %d file(s)", fileCount)

}

func (params *MyHandlers) MyFileServer(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RemoteAddr, r.Method, r.URL.Path)
	w.Header().Add("Cache-Control", "no-cache")
	contentType := mime.TypeByExtension(path.Ext(r.URL.Path))
	if contentType == "" {
        contentType = "text/html" // Default for unknown types
    }
	if strings.HasSuffix(r.URL.Path, ".wasm") {
		contentType = "application/wasm"
	}
	w.Header().Set("Content-type", contentType)
	log.Println("Response Headers:")
    for name, values := range w.Header() {
        for _, value := range values {
            log.Printf("%s: %s\n", name, value)
        }
    }
	p := "." + r.URL.Path
	if p == "./" {
		p = "index.html"
	}
	p = path.Join(params.Root, p)
	http.ServeFile(w, r, p)
}

func main() {
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	handlersWithParam := &MyHandlers{Root: root}
	mux := http.NewServeMux()

	mux.HandleFunc("/", handlersWithParam.MyFileServer)
	mux.HandleFunc("/ping", ping)
	mux.HandleFunc("/upload", handlersWithParam.uploader)
	// mux.HandleFunc("/download", handlersWithParam.download)
	mux.HandleFunc("/serve", serve)

	srv := &http.Server{Addr: fmt.Sprintf(":%s", port), Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("Listen error: %s\n", err)
			stopChan <- os.Interrupt
		}
	}()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	log.Printf("Server running @ http://%s%s http://%s%s Root: %s", hostname, srv.Addr, getOutboundIP(), srv.Addr, root)
	<-stopChan //wait for SIGINT
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %+v", err)
	}
	log.Println("Server stopped")
}
