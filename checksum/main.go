package main

import (
	"crypto/sha512"
	"flag"
	"fmt"
	"io"
	"os"
)

func init() {
	flag.Usage = func() {
		fmt.Printf("Usage: %s [options] host:port\n Options:\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	for _, arg := range flag.Args() {
		s := checksum(arg)

		fmt.Printf("%s [%s]\n", s, arg)
	}

}

// generate checksum for file
func checksum(filename string) string {

	f, err := os.Open(filename)
	if err != nil {
		return ""
	}

	defer f.Close()

	h := sha512.New()

	if _, err := io.Copy(h, f); err != nil {
		return ""
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

func UnixDomainSocket() error {

	return nil
}
