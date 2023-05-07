package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/http2"
	"html"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestListen(t *testing.T) {
	listen, err := net.Listen("tcp", "127.0.0.1:6594")
	if err != nil {
		log.Println(err)
	}
	defer func() {
		listen.Close()
	}()

	log.Println(listen.Addr())

}

func TestConn(t *testing.T) {
	listen, err := net.Listen("tcp", "127.0.0.1:6594")
	if err != nil {
		log.Println(err)
		return
	}
	defer func() {
		listen.Close()
	}()

	for {
		conn, err := listen.Accept()
		if err != nil {
			log.Println(err)
		}
		go func(connect net.Conn) {
			defer connect.Close()
			var bytes []byte
			readed, err := conn.Read(bytes)
			if err != nil {
				log.Println(err)
				return
			}
			log.Println(bytes[:readed])
		}(conn)
	}

}

func TestDial(t *testing.T) {
	listen, err := net.Listen("tcp", "127.0.0.1:6594")
	if err != nil {
		t.Log(err)
		return
	}

	c := make(chan struct{})
	go func() {
		defer func() {
			c <- struct{}{}
		}()
		for {
			conn, err := listen.Accept()
			if err != nil {
				t.Log(err)
				return
			}
			go func(connect net.Conn) {
				defer func() {
					connect.Close()
					c <- struct{}{}
				}()
				buff := make([]byte, 1024)
				for {
					n, err := conn.Read(buff)
					if err != nil {
						if err != io.EOF {
							t.Error(err)
						}
						return
					}
					t.Logf("received: %q", buff[:n])
				}

			}(conn)
		}
	}()

	conn, err := net.Dial("tcp", listen.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte("abde"))
	conn.Close()
	<-c
	listen.Close()
	<-c
}

func DialerTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, c syscall.RawConn) error {
			return &net.DNSError{
				Err:         "connection timed out",
				IsTimeout:   true,
				IsTemporary: true,
				Server:      "127.0.0.1",
				Name:        address,
			}
		},
	}
	return d.Dial(network, address)
}

func TestDialer(t *testing.T) {
	conn, err := DialerTimeout("tcp", "10.0.0.0:80", time.Second*5)
	if nErr, ok := err.(net.Error); ok && err != nil {
		t.Log(err)
		t.Log(nErr.Timeout())
	}
	if conn != nil {
		defer conn.Close()
	}
}

func TestDialCancelContext(t *testing.T) {

	delay := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), delay)

	defer cancel()
	dialer := net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			time.Sleep(5*time.Second + time.Millisecond)
			return nil
		},
	}

	conn, err := dialer.DialContext(ctx, "tcp", "10.0.0.0:80")

	if err == nil {
		conn.Close()
		t.Fatal("not timeout")
	}

	nErr, ok := err.(net.Error)
	if !ok {
		t.Fatal("not network error")
	} else {
		if !nErr.Timeout() {
			t.Fatal("not timeout")
			t.Fatal(err)
		}
	}

	if ctx.Err() != context.DeadlineExceeded {
		t.Fatal("not timeout")
	}

}

func TestDialWithCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	sync := make(chan struct{})

	go func() {
		defer func() {
			sync <- struct{}{}
		}()

		dialer := net.Dialer{
			Control: func(network, address string, c syscall.RawConn) error {
				time.Sleep(time.Second)
				return nil
			},
		}

		conn, err := dialer.DialContext(ctx, "tcp", "10.0.0.0:80")

		if err != nil {
			t.Log(err)
			return
		}
		conn.Close()
		t.Error("not timeout")
	}()

	cancel()
	<-sync

	if ctx.Err() != context.Canceled {
		t.Error("not canceled")
	}
}

// book Practical Packet Analysis by
// Chris Sanders, Chris G. Truncer, and Todd Lammle
func TestMultiCancel(t *testing.T) {

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*5))

	serv, err := net.Listen("tcp", "127.0.0.1:")
	//serv.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer serv.Close()
	go func() {
		conn, errAcc := serv.Accept()
		if errAcc == nil {
			conn.Close()
		}
	}()

	dial := func(ctx context.Context, address string, response chan int, id int, wg *sync.WaitGroup) {
		defer wg.Done()
		dialer := net.Dialer{
			Control: func(network, address string, c syscall.RawConn) error {
				time.Sleep(10 * time.Second)
				return nil
			},
		}
		conn, errDial := dialer.DialContext(ctx, "tcp", address)
		if errDial != nil {
			t.Logf("dialer %d failed to dial: %v", id, errDial)
			return
		}
		conn.Close()
		select {
		case <-ctx.Done():
		case response <- id:
		}
	}

	res := make(chan int)
	wg := sync.WaitGroup{}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go dial(ctx, serv.Addr().String(), res, i, &wg)
	}
	response := -1
	select {
	case response = <-res:
	case <-time.After(time.Second * 15):
	}
	cancel()
	wg.Wait()

	close(res)

	if ctx.Err() != context.Canceled {
		t.Error("not canceled")
	}

	t.Logf("dialer %d retrieved the resource", response)
}

func TestDeadlineTest(t *testing.T) {

	sync := make(chan struct{})

	serv, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}

	defer serv.Close()

	go func() {
		conn, errAcc := serv.Accept()
		if errAcc != nil {
			t.Log(errAcc)
			return
		}
		defer func() {
			conn.Close()
			close(sync)
		}()

		errDeadLine := conn.SetDeadline(time.Now().Add(time.Second * 5))

		if errDeadLine != nil {
			t.Error(errDeadLine)
			return
		}

		buf := make([]byte, 1)
		_, errRead := conn.Read(buf)
		nErr, ok := errRead.(net.Error)

		if !ok || !nErr.Timeout() {
			t.Error(errRead)
			return
		}

		sync <- struct{}{}

		errDeadLine = conn.SetDeadline(time.Now().Add(time.Second * 5))

		if errDeadLine != nil {
			t.Error(errDeadLine)
			return
		}

		_, errRead = conn.Read(buf)
		if errRead != nil {
			t.Error(errRead)
		}

	}()

	conn, err := net.Dial("tcp", serv.Addr().String())

	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	<-sync

	_, err = conn.Write([]byte("1"))
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err != io.EOF {
		t.Error(err)
	}
}

var defaultInterval = 5 * time.Second

func Pinger(ctx context.Context, w io.Writer, reset <-chan time.Duration) {
	var interval time.Duration
	select {
	case <-ctx.Done():
		return
	case interval = <-reset:
	default:
	}
	if interval <= 0 {
		interval = defaultInterval
	}
	timer := time.NewTimer(interval)
	defer func() {
		if !timer.Stop() {
			<-timer.C
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case newInterval := <-reset:
			if !timer.Stop() {
				<-timer.C
			}
			if newInterval > 0 {
				interval = newInterval
			}
			if newInterval < 0 {
				interval = defaultInterval
			}
		case <-timer.C:
			if _, err := fmt.Fprintf(w, "ping"); err != nil {
				log.Printf("failed to write ping: %v", err)
				return
			}
		}

		timer.Reset(interval)

	}

}

func TestExamplePinger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan struct{})
	reset := make(chan time.Duration, 1)
	reset <- time.Second

	go func() {
		defer close(done)
		Pinger(ctx, writer, reset)
	}()

	receiverPinger := func(interval time.Duration, r io.Reader) {
		if interval >= 0 {
			fmt.Printf("resset interval to %v\n", interval)
			reset <- interval
		} else {
			fmt.Printf("defaultInterval \n")
			reset <- interval
		}

		now := time.Now()
		buf := make([]byte, 1024)
		n, err := r.Read(buf)
		if err != nil {
			return
		}

		fmt.Printf("received %q after %v \n", buf[:n], time.Since(now).Round(100*time.Millisecond))
	}

	for i, v := range []int64{0, 200, 300, 0, -1, -1, -1} {
		fmt.Printf("Run %d:\n", i+1)
		receiverPinger(time.Duration(v)*time.Millisecond, reader)
	}

	cancel()
	<-done

}
func AddSlice(a []int) {
	a[1] = 3
	fmt.Printf("after add a %v\n", a)

}
func TestAddSlice(t *testing.T) {
	ints := []int{1, 2}

	AddSlice(ints)
	fmt.Printf("%v\n", ints)

}

func TestAddMap(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	AddMap(m)
	fmt.Printf("%v\n", m)
}

func AddMap(m map[string]int) {
	m["c"] = 3

	fmt.Printf("after add m %v\n", m)
}

func TestAdvancePing(t *testing.T) {
	done := make(chan struct{})
	serv, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}

	begin := time.Now()
	go func() {
		defer func() {
			close(done)
		}()
		conn, errAcc := serv.Accept()
		if errAcc != nil {
			t.Log(errAcc)
			return
		}

		innerCtx, innerCancel := context.WithCancel(context.Background())

		defer func() {
			innerCancel()
			conn.Close()
		}()

		resetChan := make(chan time.Duration, 1)
		resetChan <- time.Second
		go Pinger(innerCtx, conn, resetChan)

		errSetDeadline := conn.SetDeadline(time.Now().Add(time.Second * 5))
		if errSetDeadline != nil {
			t.Error(errSetDeadline)
			return
		}

		buf := make([]byte, 1024)

		for {
			n, errRead := conn.Read(buf)
			if errRead != nil {
				return
			}
			fmt.Printf("[%v] %q \n", time.Since(begin).Round(time.Second), buf[:n])
			resetChan <- time.Second

			errSetDeadline = conn.SetDeadline(time.Now().Add(time.Second * 5))
			if errSetDeadline != nil {
				t.Error(errSetDeadline)
				return
			}

		}
	}()

	conn, err := net.Dial("tcp", serv.Addr().String())
	if err != nil {
		t.Fatal(err)
		return
	}

	defer func() {
		conn.Close()
	}()

	buf := make([]byte, 1024)

	for i := 0; i < 4; i++ {
		n, errRead := conn.Read(buf)
		if errRead != nil {
			t.Error(errRead)
			return
		}
		fmt.Printf("[%v] %q \n", time.Since(begin).Round(time.Second), buf[:n])

		_, errWrite := conn.Write([]byte("Pong!"))
		if errWrite != nil {
			t.Error(errWrite)
			return
		}
	}

	for i := 0; i < 4; i++ {
		n, errRead := conn.Read(buf)
		if errRead != nil {
			if errRead != io.EOF {
				t.Error(errRead)
			}
			break
		}
		fmt.Printf("[%v] %q \n", time.Since(begin).Round(time.Second), buf[:n])
	}

	<-done
	end := time.Since(begin).Truncate(time.Second)
	t.Logf("[%v] s", end)
	if end != 9*time.Second {
		t.Fatalf("expected EOF at 9 seconds; actual %s", end)
	}
}

func TestReadIntoBuffer(t *testing.T) {
	buf := make([]byte, 1<<24)
	_, err := rand.Read(buf)
	if err != nil {
		t.Fatal(err)
	}

	serv, err := net.Listen("tcp", "127.0.0.1:")

	if err != nil {
		t.Fatal(err)
	}

	go func() {
		conn, errAcc := serv.Accept()
		if errAcc != nil {
			t.Log(errAcc)
			return
		}
		defer conn.Close()
		_, errWrite := conn.Write(buf)
		if errWrite != nil {
			t.Error(errWrite)
			return
		}
	}()

	conn, err := net.Dial("tcp", serv.Addr().String())

	defer conn.Close()
	if err != nil {
		t.Fatal(err)
		return
	}

	for {
		readBuf := make([]byte, 1<<19)
		n, err := conn.Read(readBuf)
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		t.Logf("read %d bytes", n)
		//t.Log(readBuf[:n])
	}
}

const wordPayload = "Hello world"

func TestScanner(t *testing.T) {

	serv, err := net.Listen("tcp", "127.0.0.1:")

	if err != nil {
		t.Fatal(err)
	}

	go func() {
		conn, err2 := serv.Accept()
		if err2 != nil {
			t.Log(err2)
			return
		}
		defer conn.Close()

		conn.Write([]byte(wordPayload))

	}()

	conn, err := net.Dial("tcp", serv.Addr().String())
	if err != nil {
		t.Fatal(err)
		return
	}

	defer conn.Close()

	scanner := bufio.NewScanner(conn)

	words := make([]string, 0)

	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		words = append(words, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		t.Error(err)
	}

	wordsExpect := []string{"Hello", "world"}
	if !reflect.DeepEqual(words, wordsExpect) {
		t.Fatalf("expected %v; actual %v", wordsExpect, words)
	}
	t.Logf("words: %v", words)
}

const (
	BinaryType uint8 = iota + 1
	StringType
	MaxPayloadSize uint32 = 10 << 20
)

var (
	ErrMaxPayloadSize = errors.New("max payload size")
)

type Payload interface {
	fmt.Stringer
	io.WriterTo
	io.ReaderFrom
	Bytes() []byte
}

type Binary []byte

func (b Binary) Bytes() []byte {
	return []byte(b)
}

func (b Binary) String() string {
	return string(b)
}

func (b Binary) WriteTo(w io.Writer) (int64, error) {

	err := binary.Write(w, binary.BigEndian, BinaryType)
	if err != nil {
		return 0, err
	}

	var n int64 = 1

	err = binary.Write(w, binary.BigEndian, uint32(len(b)))

	if err != nil {
		return n, err
	}

	n += 4

	nw, err := w.Write(b)
	return n + int64(nw), nil
}

func (b *Binary) ReadFrom(r io.Reader) (int64, error) {
	var n int64
	var t uint8
	err := binary.Read(r, binary.BigEndian, &t)

	if err != nil {
		return -1, err
	}

	if t != BinaryType {
		return -1, errors.New("invalid type")
	}

	n = 1

	var size uint32
	err = binary.Read(r, binary.BigEndian, &size)

	if err != nil {
		return -1, err
	}

	n += 4
	if uint32(size) > MaxPayloadSize {
		return -1, ErrMaxPayloadSize
	}

	*b = make([]byte, size)

	nr, err := io.ReadFull(r, *b)

	if err != nil {
		return -1, err
	}

	n += int64(nr)

	return n, nil
}

type String string

func (s String) Bytes() []byte {
	return []byte(s)
}

func (s String) String() string {
	return string(s)
}

func (s String) WriteTo(w io.Writer) (int64, error) {

	err := binary.Write(w, binary.BigEndian, StringType)
	if err != nil {
		return 0, err
	}

	var n int64 = 1

	err = binary.Write(w, binary.BigEndian, uint32(len(s)))

	if err != nil {
		return n, err
	}

	n += 4

	nw, err := w.Write([]byte(s))
	return n + int64(nw), nil
}

func (s *String) ReadFrom(r io.Reader) (int64, error) {
	var n int64
	var t uint8
	err := binary.Read(r, binary.BigEndian, &t)

	if err != nil {
		return -1, err
	}

	if t != StringType {
		return -1, errors.New("invalid type")
	}

	n = 1

	var size uint32
	err = binary.Read(r, binary.BigEndian, &size)

	if err != nil {
		return -1, err
	}

	n += 4
	if uint32(size) > MaxPayloadSize {
		return -1, ErrMaxPayloadSize
	}

	buf := make([]byte, size)

	nr, err := io.ReadFull(r, buf)

	if err != nil {
		return -1, err
	}

	*s = String(buf)

	n += int64(nr)

	return n, nil
}

func decode(r io.Reader) (Payload, error) {
	var t uint8
	err := binary.Read(r, binary.BigEndian, &t)

	if err != nil {
		return nil, err
	}

	var p Payload

	switch t {
	case BinaryType:
		p = new(Binary)
	case StringType:
		p = new(String)
	default:
		return nil, errors.New("invalid type")
	}

	_, err = p.ReadFrom(io.MultiReader(bytes.NewReader([]byte{t}), r))

	if err != nil {
		return nil, err
	}

	return p, nil
}

func TestPayload(t *testing.T) {

	b1 := Binary("Hello world")
	b2 := Binary("Payload test")
	s1 := String("good morning")

	payloads := []Payload{&b1, &b2, &s1}

	serv, err := net.Listen("tcp", "127.0.0.1:")

	if err != nil {
		t.Fatal(err)
		return
	}
	go func() {
		conn, err2 := serv.Accept()
		if err2 != nil {
			t.Log(err2)
			return
		}
		defer conn.Close()

		for _, p := range payloads {
			_, err := p.WriteTo(conn)
			if err != nil {
				t.Error(err)
				break
			}
		}
	}()

	conn, err := net.Dial("tcp", serv.Addr().String())
	if err != nil {
		t.Fatal(err)
		return
	}

	defer conn.Close()

	for _, p := range payloads {
		p2, err := decode(conn)
		if err != nil {
			if err == io.EOF {
				t.Log("EOF")
				return
			}
			t.Error(err)
		}
		if !reflect.DeepEqual(p, p2) {
			t.Fatalf("expected %v; actual %v", p, p2)
		}
		fmt.Printf("%v\n", p2)
	}
}

func TestMaxPayload(t *testing.T) {
	buf := new(bytes.Buffer)
	err := buf.WriteByte(BinaryType)

	if err != nil {
		t.Fatal(err)
	}

	err = binary.Write(buf, binary.BigEndian, uint32(1<<30))

	if err != nil {
		t.Fatal(err)
	}

	var b Binary
	_, err = b.ReadFrom(buf)

	if err != ErrMaxPayloadSize {
		t.Fatal(err)
	}
}

func proxy(w io.Writer, r io.Reader) error {
	reader, isReader := w.(io.Reader)
	writer, isWriter := r.(io.Writer)

	fmt.Printf("isReader: %v, isWriter: %v\n", isReader, isWriter)
	if isWriter && isReader {
		go func() {
			_, _ = io.Copy(writer, reader)
		}()
	}

	_, err := io.Copy(w, r)
	return err
}

func TestProxy(t *testing.T) {

	serv, err := net.Listen("tcp", "127.0.0.1:")

	if err != nil {
		t.Fatal(err)
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {

		defer wg.Done()
		for {
			conn, err := serv.Accept()
			if err != nil {
				return
			}

			go func(conn net.Conn) {
				defer conn.Close()

				buf := make([]byte, 1024)

				for {
					n, err := conn.Read(buf)
					if err != nil {
						if err != io.EOF {
							t.Fatal(err)
						}
						return
					}
					msg := string(buf[:n])
					t.Log(msg)
					switch msg {
					case "ping":
						_, err = conn.Write([]byte("pong"))
					default:
						_, err = conn.Write(buf[:n])
					}

					if err != nil {
						if err != io.EOF {
							t.Fatal(err)
						}
						return
					}
				}
			}(conn)
		}
	}()

	proxyServ, err := net.Listen("tcp", "127.0.0.1:")

	if err != nil {
		t.Fatal(err)
		return
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		for {
			conn, err := proxyServ.Accept()
			if err != nil {
				return
			}

			go func(from net.Conn) {
				defer from.Close()

				to, err := net.Dial("tcp", serv.Addr().String())
				if err != nil {
					t.Fatal(err)
					return
				}
				defer to.Close()

				err = proxy(to, from)
				if err != nil && err != io.EOF {
					t.Error(err)
				}

				conn.RemoteAddr().Network()
			}(conn)
		}
	}()

	conn, err := net.Dial("tcp", proxyServ.Addr().String())
	if err != nil {
		t.Fatal(err)
		return
	}

	testCases := []struct{ Message, Reply string }{
		{"ping", "pong"},
		{"hello", "hello"},
		{"world", "world"},
	}

	for _, testCase := range testCases {

		t.Logf("send: %s\n", testCase.Message)
		_, err := conn.Write([]byte(testCase.Message))
		if err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("reply: %s\n", string(buf[:n]))

		if string(buf[:n]) != testCase.Reply {
			t.Fatalf("expected %s; actual %s", testCase.Reply, string(buf[:n]))
		}
	}

	conn.Close()
	proxyServ.Close()
	serv.Close()

	wg.Wait()
}

type Monitor struct {
	*log.Logger
}

func (m Monitor) Write(p []byte) (int, error) {
	length := 0
	length = length + len(p)
	m.Output(2, string(p))
	return length, nil
}

func TestMonitor(t *testing.T) {

	m := Monitor{log.New(os.Stdout, "monitor: ", 0)}

	serv, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}
	defer serv.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := serv.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)

		r := io.TeeReader(conn, m)

		n, err := r.Read(buf)
		if err != nil && err != io.EOF {
			m.Println(err)
			return
		}

		w := io.MultiWriter(conn, m)

		_, err = w.Write(buf[:n])

		if err != nil && err != io.EOF {
			m.Println(err)
			return
		}
	}()

	conn, err := net.Dial("tcp", serv.Addr().String())
	if err != nil {
		m.Fatal(err)
	}

	_, err = conn.Write([]byte("hello"))

	conn.Close()
	<-done
}

func TestTcp(t *testing.T) {

	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:9910")

	if err != nil {
		t.Fatal(err)

	}

	baiduAddr, err := net.ResolveTCPAddr("tcp", "www.baidu.com:80")

	if err != nil {
		t.Fatal(err)
	}

	baiduConn, err := net.DialTimeout("tcp", baiduAddr.String(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer baiduConn.Close()

	tcp, err := net.ListenTCP("tcp", addr)

	done := make(chan struct{})
	go func() {
		defer close(done)
		acceptTCP, err := tcp.AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		defer acceptTCP.Close()

		acceptTCP.SetKeepAlive(true)
		acceptTCP.SetKeepAlivePeriod(10 * time.Second)
		acceptTCP.SetLinger(10)
		acceptTCP.SetWriteBuffer(2000)
		acceptTCP.SetReadBuffer(20000)
	}()

	dialTCP, err := net.DialTCP("tcp", nil, addr)

	if err != nil {
		t.Fatal(err)
	}

	dialTCP.Close()
	<-done
	tcp.Close()

}

func TestUdp(t *testing.T) {

	servAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9910}
	udp, err := net.ListenUDP("udp", servAddr)

	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		for {
			n, addr, err := udp.ReadFromUDP(buf)
			if err != nil {
				return
			}
			readStr := string(buf[:n])
			t.Log(n, addr, readStr)
			if readStr == "end" {
				break
			}

			_, err = udp.WriteToUDP(buf[:n], addr)
			if err != nil {
				return
			}
		}

	}()

	clientConn, err := net.ListenPacket("udp", "127.0.0.1:")

	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct{ Message, Reply string }{
		{"ping", "ping"},
		{"hello", "hello"},
		{"world", "world"},
		{"end", "end"},
	}

	for _, testCase := range testCases {

		_, err = clientConn.WriteTo([]byte(testCase.Message), servAddr)
		if err != nil {
			t.Fatal(err)
		}

		if testCase.Message == "end" {
			break
		}

		buf := make([]byte, 1024)
		read, addr, err := clientConn.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("received: ", string(buf[:read]))

		if addr.String() != servAddr.String() {
			t.Fatal("addr not match")
		}
	}

	clientConn.Close()

	<-done
}

const (
	i = 7
	j
	k
)

func TestConst(t *testing.T) {
	fmt.Printf("%d %d %d", i, j, k)
}

func TestSyncMap(t *testing.T) {
	var m sync.Map
	m.Store("address", map[string]string{"province": "江苏", "city": "南京"})
	v, _ := m.Load("address")
	m2 := v.(map[string]string)
	fmt.Println(m2["province"])
	a := v
	switch i := a.(type) {
	case int:
		fmt.Println("int")
		fmt.Println(i)
	case byte:
		fmt.Println("byte")
		fmt.Println(i)
	}
}

func TestA(t *testing.T) {
	num := 6
	for index := 0; index < num; index++ {
		resp, _ := http.Get("https://www.baidu.com")
		_, _ = ioutil.ReadAll(resp.Body)
	}
	fmt.Printf("此时goroutine个数= %d\n", runtime.NumGoroutine())
}

func TestChannel(t *testing.T) {

	number := make(chan int)
	alphabet := make(chan int)
	done := make(chan struct{})

	wg := sync.WaitGroup{}

	go func() {

		al := 1
		for {
			select {
			case <-number:
				fmt.Println(al)
				al++
				fmt.Println(al)
				al++
				alphabet <- 0
			case <-done:
				return
			}
		}

	}()
	wg.Add(1)

	go func() {
		al := 'A'
		defer wg.Done()
		for {
			select {
			case <-alphabet:
				if al > 25+'A' {
					close(done)
					return
				}
				fmt.Println(string(al))
				al++
				fmt.Println(string(al))
				al++
				number <- 0
			}
		}
	}()

	number <- 0
	wg.Wait()
}

func echoServe(ctx context.Context, network, addr string) (net.Addr, error) {
	serv, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}
	go func() {
		go func() {
			<-ctx.Done()
			fmt.Printf("server close on %s\n", serv.Addr().String())
			err = serv.Close()
			if err != nil {
				return
			}
		}()
		for {
			conn, err2 := serv.Accept()
			if err2 != nil {
				return
			}
			go func() {
				defer conn.Close()

				b := make([]byte, 1024)
				n, err := conn.Read(b)
				if err != nil {
					return
				}

				_, err = conn.Write(b[:n])

				if err != nil {
					return
				}
			}()
		}
	}()
	return serv.Addr(), nil

}

func TestUnix(t *testing.T) {

	temp, err3 := os.MkdirTemp("", "unix_tmp")

	if err3 != nil {
		t.Fatal(err3)
	}

	defer func() {
		err := os.RemoveAll(temp)
		if err != nil {
			t.Fatal(err)
		}
	}()

	// join socket path and "echo.sock" to create a full path
	socket := filepath.Join(temp, fmt.Sprintf("%d.socket", os.Getpid()))

	fmt.Printf("socket: %s\n", socket)
	defer func() {
		err2 := os.RemoveAll(socket)
		if err2 != nil {
			t.Fatal(err2)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	serve, err := echoServe(ctx, "unix", socket)
	if err != nil {
		t.Fatal(err)
	}

	err2 := os.Chmod(socket, os.ModeSocket|0666)
	if err2 != nil {
		t.Fatal(err2)
	}
	fmt.Println(serve)
	conn, err := net.Dial("unix", serve.String())
	for i := 0; i < 3; i++ {
		_, err := conn.Write([]byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
	}

	b := make([]byte, 1024)
	n, err := conn.Read(b)
	if err != nil {
		t.Fatal(err)
	}

	expected := bytes.Repeat([]byte("hello"), 3)
	// compare the expected and actual
	if !bytes.Equal(expected, b[:n]) {
		t.Fatalf("expected %s, got %s", expected, b[:n])
	}
	cancel()
}

func TestTpc(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	serve, err := echoServe(ctx, "tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(serve)
	conn, err := net.Dial("tcp", serve.String())
	for i := 0; i < 3; i++ {
		_, err := conn.Write([]byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 3; i++ {
		b := make([]byte, 1024)
		n, err := conn.Read(b)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("read: %s\n", b[:n])
	}

	cancel()
}

func TestHttp(t *testing.T) {

	response, err := http.Head("https://time.gov")

	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()

	now := time.Now().Round(time.Second)

	date := response.Header.Get("Date")

	if date == "" {
		t.Fatal("no date header")
	}

	dt, err := time.Parse(time.RFC1123, date)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("now: %v, dt: %v", now.Sub(dt), dt)
}

func blockIndefinitely(w http.ResponseWriter, r *http.Request) {
	select {}

}

func TestBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(blockIndefinitely))
	_, _ = http.Get(server.URL)
	t.Fatal("client did not indefinitely block")
}

func TestBlockWithTimeout(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(blockIndefinitely))

	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(timeout, http.MethodGet, server.URL, nil)

	resp, err := http.DefaultClient.Do(request)

	if err != nil {
		b := errors.Is(err, context.DeadlineExceeded)
		if !b {
			t.Fatal(err)
		}
	}
	defer resp.Body.Close()
	t.Fatal("client did not timeout")

}

func TestBlockWithCancel(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(blockIndefinitely))

	timeout, cancel := context.WithCancel(context.Background())
	afterFunc := time.AfterFunc(5*time.Second, cancel)
	request, err := http.NewRequestWithContext(timeout, http.MethodGet, server.URL, nil)

	resp, err := http.DefaultClient.Do(request)

	if err != nil {
		b := errors.Is(err, context.DeadlineExceeded)
		if !b {
			t.Fatal(err)
		}
	}
	afterFunc.Reset(5 * time.Second)
	defer resp.Body.Close()
	t.Fatal("client did not timeout")

}

type Name struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

func handlePostRequest(t *testing.T) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func(r io.ReadCloser) {
			_, _ = io.Copy(io.Discard, r)
			_ = r.Close()
		}(r.Body)
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var name Name
		err := json.NewDecoder(r.Body).Decode(&name)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Printf("name: %v\n", name)
	}

}

func TestJson(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(handlePostRequest(t)))
	defer server.Close()

	name := Name{
		First: "John",
		Last:  "Doe",
	}

	b, err := json.Marshal(name)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(server.URL, "application/json", bytes.NewBuffer(b))
	if err != nil {
		t.Fatal(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatal("status not ok")
	}

}

func TestMultipart(t *testing.T) {

	b := new(bytes.Buffer)
	w := multipart.NewWriter(b)
	err := w.SetBoundary("abcdedfghijklmnopqrstuvwxyz")
	if err != nil {
		t.Fatal(err)
	}

	m := map[string]string{
		"first": "John",
		"last":  "Doe",
		"date":  time.Now().Format(time.RFC3339),
	}

	for k, v := range m {

		err := w.WriteField(k, v)
		if err != nil {
			t.Fatal(err)
		}

	}

	for i, file := range []string{
		"./file/file1.txt",
		"./file/file2.txt",
	} {
		filepart, err := w.CreateFormFile(fmt.Sprintf("file%d", i), filepath.Base(file))
		if err != nil {
			t.Fatal(err)
		}

		file, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}

		_, err = io.Copy(filepart, file)
		if err != nil {
			t.Fatal(err)
		}

		err = file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}

	timeout, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()

	request, err := http.NewRequestWithContext(timeout, http.MethodPost, "https://httpbin.org/post", b)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(request)

	if err != nil {
		t.Fatal(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatal("status not ok")
	}

	rb, err := io.ReadAll(resp.Body)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("body: %s\n", rb)
}

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("hello world"))
	_ = r.Body.Close()
}

func TestMultiplexer(t *testing.T) {

	server := &http.Server{
		Addr:              "127.0.0.1:8443",
		Handler:           http.TimeoutHandler(http.HandlerFunc(defaultHandler), 5*time.Second, "server timed out"),
		IdleTimeout:       5 * time.Second,
		ReadHeaderTimeout: time.Second,
	}

	serv, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		err := server.ServeTLS(serv, "", "")
		if err != http.ErrServerClosed {
			t.Error(err)
		}
	}()

	resp, err := http.Get("http://" + server.Addr)

	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatal("status not ok")
	}

	rb, err := io.ReadAll(resp.Body)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("body: %s\n", rb)
}

func TestMultiplexerTLS(t *testing.T) {

	server := &http.Server{
		Addr:              "127.0.0.1:8443",
		Handler:           http.TimeoutHandler(http.HandlerFunc(defaultHandler), 5*time.Second, "server timed out"),
		IdleTimeout:       5 * time.Second,
		ReadHeaderTimeout: time.Second,
	}

	serv, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		err := server.ServeTLS(serv, "", "")
		if err != http.ErrServerClosed {
			t.Error(err)
		}
	}()

	resp, err := http.Get("http://" + server.Addr)

	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatal("status not ok")
	}

	rb, err := io.ReadAll(resp.Body)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("body: %s\n", rb)
}

func getMacAddr() ([]string, error) {
	ifas, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var as []string
	for _, ifa := range ifas {
		a := ifa.HardwareAddr.String()
		if a != "" {
			as = append(as, a)
		}
	}
	return as, nil
}

func TestGetMac(t *testing.T) {

	addrs, err := getMacAddr()

	if err != nil {
		t.Fatal(err)
	}

	for _, addr := range addrs {
		t.Logf("addr: %s\n", addr)
	}

}

func TestHandlerWrite(t *testing.T) {

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("hello world"))
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Log("status not ok")
	}

	defer func() {
		_ = request.Body.Close()
	}()
	t.Logf("body: %s\n", recorder.Body.String())

	handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello world"))
		w.WriteHeader(http.StatusBadRequest)
	}

	request = httptest.NewRequest(http.MethodGet, "http://localhost", nil)
	recorder = httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Log("status not ok")
	}

	defer func() {
		_ = request.Body.Close()
	}()

	t.Logf("body: %s\n", recorder.Body.String())

}

type Methods map[string]http.Handler

func (m Methods) ServeHTTP(w http.ResponseWriter, request *http.Request) {

	defer func(r io.ReadCloser) {
		_, _ = io.Copy(io.Discard, r)
		_ = r.Close()
	}(request.Body)

	if handler, ok := m[request.Method]; ok {
		if handler == nil {
			http.Error(w, "not handler", http.StatusNotFound)
		} else {
			handler.ServeHTTP(w, request)
		}
		return
	}

	w.Header().Set("Allow", m.allowMethods())

	// if request method is not options, return method not allowed
	if request.Method != http.MethodOptions {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m Methods) allowMethods() string {
	methods := make([]string, 0, len(m))
	for k := range m {
		methods = append(methods, k)
	}
	sort.Strings(methods)
	return strings.Join(methods, ", ")
}

func DefaultMethods() http.Handler {
	return Methods{

		http.MethodGet: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("hello world"))
		}),
		http.MethodPost: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func(r io.ReadCloser) {
				_, _ = io.Copy(io.Discard, r)
				_ = r.Close()
			}(r.Body)
			b, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			_, _ = fmt.Fprintf(w, "hello %s", html.EscapeString(string(b)))

		}),
	}
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

func TestRestrictPrefix(t *testing.T) {

	testcase := map[string]int{
		"/api/":   http.StatusNotFound,
		"/1234":   http.StatusOK,
		"/123444": http.StatusOK,
	}

	for k, v := range testcase {
		serv := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/"+k, nil)

		RestrictPrefix("api", DefaultMethods()).ServeHTTP(serv, request)

		if serv.Result().StatusCode != v {
			t.Fatal("testcase failed")
		}
	}

}

func drainAndClose(handle http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle.ServeHTTP(w, r)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	})
}

func TestMultiplexer2(t *testing.T) {

	serveMux := http.NewServeMux()

	serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	serveMux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello api"))
	})

	serveMux.HandleFunc("/api/helo/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello api helo"))
	}))

	mux := drainAndClose(serveMux)

	testcase := []struct {
		path     string
		response string
		code     int
	}{
		{"http://test/", "", http.StatusNoContent},
		{"http://test/api", "hello api", http.StatusOK},
		{"http://test/api/helo/", "hello api helo", http.StatusOK},
		{"http://test/api/helo", "<a href=\"/api/helo/\">Moved Permanently</a>.\n\n", http.StatusMovedPermanently},
		{"http://test/api/helo/213", "hello api helo", http.StatusOK},

		{"http://test/api/and/goodbye", "", http.StatusNoContent},
		{"http://test/api/ee/and/ee", "", http.StatusNoContent},
	}

	for _, v := range testcase {
		request := httptest.NewRequest(http.MethodGet, v.path, nil)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		r := recorder.Result()
		if r.StatusCode != v.code {
			t.Log(v)
			t.Fatal("code not match")

		}
		b, err := io.ReadAll(recorder.Body)

		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()

		if actual := string(b); actual != v.response {
			t.Log(v)
			t.Fatal("response not match")
		}
	}
}

func TestClientTLS(t *testing.T) {

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			u := "https://" + r.Host + r.URL.String()
			http.Redirect(w, r, u, http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL)

	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("status code not match")
	}

	tp := &http.Transport{
		TLSClientConfig: &tls.Config{
			CurvePreferences: []tls.CurveID{tls.CurveP256},
			MinVersion:       tls.VersionTLS12,
		},
	}

	// config transport
	err = http2.ConfigureTransport(tp)

	if err != nil {
		t.Fatal(err)
	}

	client2 := &http.Client{

		Transport: tp,
	}

	resp, err = client2.Get(ts.URL)

	if err == nil || !strings.Contains(err.Error(), "certificate signed by unknown authority") {
		t.Fatalf("expecting unknown authority error; actual: %q\n", err)
	}

	tp.TLSClientConfig.InsecureSkipVerify = true
	resp, err = client2.Get(ts.URL)

	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expecting status OK; actual: %q\n", resp.Status)
	}
}

func TestTLSBaidu(t *testing.T) {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 30 * time.Second},
		"tcp",
		"www.baidu.com:443",
		&tls.Config{
			CurvePreferences: []tls.CurveID{tls.CurveP256},
			MinVersion:       tls.VersionTLS12,
		},
	)

	if err != nil {
		t.Fatal(err)
	}

	state := conn.ConnectionState()

	t.Logf("TLS 1.%d", state.Version-tls.VersionTLS10)
	t.Log(tls.CipherSuiteName(state.CipherSuite))
	t.Log(state.VerifiedChains[0][0].Issuer.Organization[0])

	_ = conn.Close()
}
