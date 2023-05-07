package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	address = flag.String("a", "127.0.0.1:69", "listen address")
	payload = flag.String("p", "payload.jpg", "file to serve client")
)

func init() {
	flag.Usage = func() {
		fmt.Printf("Usage: %s [options] host:port\n Options:\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	file, err2 := os.ReadFile(*payload)
	if err2 != nil {
		log.Fatal(err2)
	}
	tftp := Server{Payload: file}

	err := tftp.ListenAndServe(*address)

	if err != nil {
		log.Fatal(err2)
	}

}
