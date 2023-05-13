package main

import (
	housework "chore"
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
)

var addr, caCertFn string

func init() {
	flag.StringVar(&addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&caCertFn, "ca-cert", "serverCert.pem", "path to TLS certificate")

}
func main() {

	flag.Parse()

	args := flag.Args()

	argsLen := len(args)

	caCert, err := os.ReadFile(caCertFn)
	if err != nil {
		log.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caCert)

	client, err := grpc.Dial(addr, grpc.WithTransportCredentials(
		credentials.NewTLS(&tls.Config{
			CurvePreferences: []tls.CurveID{tls.CurveP256},
			MinVersion:       tls.VersionTLS12,
			RootCAs:          pool,
		})))

	if err != nil {
		log.Fatal(err)
	}

	rosie := housework.NewRobotMaidClient(client)
	ctx := context.Background()

	switch strings.ToLower(flag.Arg(0)) {
	default:

		if err := list(ctx, rosie); err != nil {
			log.Fatal(err)
		}

	case "add":
		if argsLen < 2 {
			log.Fatal("must specify a description")
		}

		if err := add(ctx, rosie, strings.Join(args[1:], " ")); err != nil {
			log.Fatal(err)
		}

	case "complete":
		if argsLen < 2 {
			log.Fatal("must specify a chore number")
		}

		if err := complete(ctx, rosie, args[1]); err != nil {
			log.Fatal(err)
		}
	}
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

func list(ctx context.Context, client housework.RobotMaidClient) error {
	chores, err := client.List(ctx, &housework.Empty{})

	if err != nil {
		return err
	}

	if len(chores.Chores) == 0 {
		fmt.Printf("no chores\n")
		return nil
	}

	fmt.Printf("#\t[X]\tDescription\n")
	for i, c := range chores.Chores {
		complete := " "
		if c.Complete == true {
			complete = "X"
		}

		fmt.Printf("%d\t[%s]\t%s\n", i+1, complete, c.Description)
	}

	return nil
}

func add(ctx context.Context, client housework.RobotMaidClient, description string) error {
	cs := new(housework.Chores)

	for _, chore := range strings.Split(description, ",") {
		if desc := strings.TrimSpace(chore); desc != "" {
			cs.Chores = append(cs.Chores, &housework.Chore{Description: chore})
		}
	}

	if len(cs.Chores) == 0 {
		return fmt.Errorf("no chores")
	}

	_, err := client.Add(ctx, cs)

	return err

}

func complete(ctx context.Context, client housework.RobotMaidClient, choreNumber string) error {

	i, err := strconv.Atoi(choreNumber)

	if err != nil {
		return err
	}

	_, err = client.Complete(ctx, &housework.CompleteRequest{ChoreNumber: int32(i)})

	return err

}
