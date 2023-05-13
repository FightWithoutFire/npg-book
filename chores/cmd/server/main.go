package main

import (
	housework "chore"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"io"
	"log"
	"net"
)

var addr, certFn, keyFn string

func init() {
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&certFn, "cert", "serverCert.pem", "path to TLS certificate")
	flag.StringVar(&keyFn, "key", "serverKey.pem", "path to TLS key")

}
func main() {

	flag.Parse()

	server := grpc.NewServer()
	rosie := new(housework.Rosie)

	housework.RegisterRobotMaidServer(server, rosie)

	cert, err := tls.LoadX509KeyPair(certFn, keyFn)
	if err != nil {
		log.Fatal(err)
	}

	listen, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("listening on %s\n", addr)

	log.Fatal(server.Serve(tls.NewListener(listen, &tls.Config{
		Certificates:     []tls.Certificate{cert},
		CurvePreferences: []tls.CurveID{tls.CurveP256},
		MinVersion:       tls.VersionTLS12,
	})))
}

func Load(r io.Reader) (cs []*housework.Chore, err error) {

	b, err := io.ReadAll(r)

	if err != nil {
		return nil, err
	}

	var c housework.Chores

	err = proto.Unmarshal(b, &c)

	if err != nil {
		return nil, err
	}

	return c.Chores, nil
}

func Store(w io.Writer, cs []*housework.Chore) error {

	b, err := proto.Marshal(&housework.Chores{Chores: cs})

	if err != nil {
		return err
	}

	_, err = w.Write(b)

	if err != nil {
		return err
	}

	return nil
}
