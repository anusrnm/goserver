package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//Build executable without cmd window
//go build -ldflags -H=windowsgui
// to reduce binary size:
//go build -ldflags="-s -w"

//go:embed static/*
var staticFiles embed.FS

var port string
var uploadDir string
// var showIp bool

func init() {
	flag.StringVar(&port, "p", "5000", "port to listen")
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
	UploadDir string
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

	if r.Method != http.MethodPost {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
        return
    }
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Processing error", http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}
	// Ensure upload directory exists
	if err := os.MkdirAll(params.UploadDir, 0755); err != nil {
		http.Error(w, "Unable to create upload directory", http.StatusInternalServerError)
		log.Print(err.Error())
		return
	}
	fileCount := 0
	results := []string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if part.FileName() == "" {
			continue
		}
		fullFileName := filepath.Join(params.UploadDir, part.FileName())
		dst, err := os.Create(fullFileName)
		if err != nil {
			results = append(results, part.FileName()+": error saving file")
			continue
		}
		_, err = io.Copy(dst, part)
		dst.Close()
		if err != nil {
			results = append(results, part.FileName()+": error writing file")
			continue
		}
		fileCount++
		results = append(results, part.FileName()+": uploaded")
	}
	fmt.Fprintf(w, "Uploaded %d file(s)\n%s", fileCount, strings.Join(results, "\n"))
	log.Printf("Saved %d file(s)", fileCount)
}

// serveIndex serves the index.html file for the root URL
func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		// Serve the index.html file
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	// Serve other static files
	http.FileServer(http.FS(staticFiles)).ServeHTTP(w, r)
}

func main() {
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	handlersWithParam := &MyHandlers{UploadDir: uploadDir}
	mux := http.NewServeMux()

	mux.HandleFunc("/", serveIndex)
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
	log.Printf("Server running @ http://%s%s http://%s%s. UploadDir: %s", hostname, srv.Addr, getOutboundIP(), srv.Addr, uploadDir)
	<-stopChan //wait for SIGINT
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %+v", err)
	}
	log.Println("Server stopped")
}
