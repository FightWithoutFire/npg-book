package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"time"
)

var (
	addr  = flag.String("listen", "127.0.0.1:8080", "listen address")
	cert  = flag.String("cert", "", "certificate")
	pkey  = flag.String("key", "", "private key")
	files = flag.String("files", "./files", "static file directory")
)

func main() {
	flag.Parse()

	err := run(*addr, *cert, *pkey, *files)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Server gracefully shutdown")

}

func run(addr string, cert string, pkey string, files string) error {

	mux := http.NewServeMux()
	mux.Handle("/static/",
		http.StripPrefix("/static/", RestrictPrefix(".", http.FileServer(http.Dir(files)))))

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pusher, ok := w.(http.Pusher)
		if ok {
			targets := []string{"/static/style.css", "/static/hiking.svg"}
			for _, target := range targets {
				if err := pusher.Push(target, nil); err != nil {
					log.Printf("Failed to push: %v", err)
				}
			}
		}

		http.ServeFile(w, r, path.Join(files, "index.html"))
	}))

	mux.Handle("/2", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path.Join(files, "index2.html"))
	}))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		IdleTimeout:       time.Minute,
		ReadHeaderTimeout: 30 * time.Second,
	}

	done := make(chan struct{})
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		for {
			if <-c == os.Interrupt {
				log.Println("Server shutting down...")

				err := srv.Shutdown(context.Background())
				if err != nil {
					log.Printf("Server shutdown error: %v", err)
				}
				close(done)
				return
			}
		}
	}()

	log.Printf("Server listening on %s", addr)

	var err error

	if cert != "" && pkey != "" {
		err = srv.ListenAndServeTLS(cert, pkey)
	} else {
		err = srv.ListenAndServe()
	}

	if err == http.ErrServerClosed {
		err = nil
	}

	<-done
	return err
}

func RestrictPrefix(prefix string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, k := range strings.Split(path.Clean(r.URL.Path), "/") {
			if strings.HasPrefix(k, prefix) {
				http.NotFound(w, r)
				return
			}
		}

		handler.ServeHTTP(w, r)

	})
}
