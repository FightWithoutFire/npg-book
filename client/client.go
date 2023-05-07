package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

const (
	DatagramSize = 516
	BlockSize    = DatagramSize - 4
)

type OpCode uint16

const (
	OpRRQ OpCode = iota + 1
	_            // OpCodeWrite
	OpData
	OpACK
	OpERR
)

type ErrCode uint16

const (
	ErrUnknown ErrCode = iota + 1
	ErrNotFound
	ErrAccessViolation
	ErrDiskFull
	ErrIllegalOperation
	ErrUnknownTransferID
	ErrFileAlreadyExists
	ErrNoSuchUser
)

type ReadReq struct {
	FileName string
	Mode     string
}

func (q ReadReq) MarshalBinary() ([]byte, error) {

	mode := "octet"
	if q.Mode != "" {
		mode = q.Mode
	}

	cap := 2 + 2 + len(q.FileName) + 1 + len(q.Mode) + 1
	b := new(bytes.Buffer)
	b.Grow(cap)

	err := binary.Write(b, binary.BigEndian, OpRRQ)
	if err != nil {
		return nil, err
	}

	_, err = b.WriteString(q.FileName)
	if err != nil {
		return nil, err
	}

	err = b.WriteByte(0)
	if err != nil {
		return nil, err
	}

	_, err = b.WriteString(mode)
	if err != nil {
		return nil, err
	}

	err = b.WriteByte(0)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (q *ReadReq) UnmarshalBinary(data []byte) error {

	r := bytes.NewBuffer(data)

	var code OpCode

	err := binary.Read(r, binary.BigEndian, &code)
	if err != nil {
		return err
	}

	if code != OpRRQ {
		return errors.New("invalid opcode")
	}

	q.FileName, err = r.ReadString(0)
	if err != nil {
		return errors.New("invalid read request")
	}

	q.FileName = strings.TrimRight(q.FileName, "\x00")
	if len(q.FileName) == 0 {
		return errors.New("invalid read request")
	}

	q.Mode, err = r.ReadString(0)
	if err != nil {
		return errors.New("invalid read request")
	}

	q.Mode = strings.TrimRight(q.Mode, "\x00")
	if len(q.Mode) == 0 {
		return errors.New("invalid read request")
	}

	actual := strings.ToLower(q.Mode)

	if actual != "octet" {
		return errors.New("invalid read request")
	}

	return nil
}

type Data struct {
	Block   uint16
	Payload io.Reader
}

func (d *Data) MarshalBinary() ([]byte, error) {

	b := new(bytes.Buffer)
	b.Grow(DatagramSize)

	d.Block++
	err := binary.Write(b, binary.BigEndian, OpData)
	if err != nil {
		return nil, err
	}

	err = binary.Write(b, binary.BigEndian, d.Block)
	if err != nil {
		return nil, err
	}

	_, err = io.CopyN(b, d.Payload, BlockSize)
	if err != nil && err != io.EOF {
		return nil, err
	}

	return b.Bytes(), nil
}

func (d *Data) UnmarshalBinary(data []byte) error {
	if l := len(data); l < 4 || l > DatagramSize {
		return errors.New("invalid data")
	}

	var opCode OpCode
	err := binary.Read(bytes.NewBuffer(data[:2]), binary.BigEndian, &opCode)
	if err != nil || opCode != OpData {
		return errors.New("invalid data")
	}

	err = binary.Read(bytes.NewBuffer(data[2:4]), binary.BigEndian, &d.Block)
	if err != nil {
		return errors.New("invalid data")
	}

	d.Payload = bytes.NewBuffer(data[4:])

	return nil
}

type Ack uint16

func (a *Ack) MarshalBinary() ([]byte, error) {

	cap := 2 + 2
	b := new(bytes.Buffer)
	b.Grow(cap)

	err := binary.Write(b, binary.BigEndian, OpACK)
	if err != nil {
		return nil, err
	}

	err = binary.Write(b, binary.BigEndian, a)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil

}

func (a *Ack) UnmarshalBinary(data []byte) error {
	var opCode OpCode
	r := bytes.NewReader(data)
	err := binary.Read(r, binary.BigEndian, &opCode)

	if err != nil {
		return err
	}
	if opCode != OpACK {
		return errors.New("invalid ack")
	}

	err = binary.Read(r, binary.BigEndian, a)

	if err != nil {
		return errors.New("invalid ack")
	}
	return nil
}

type Error struct {
	Error   ErrCode
	Message string
}

func (e *Error) MarshalBinary() ([]byte, error) {
	cap := 2 + 2 + len(e.Message) + 1
	b := new(bytes.Buffer)
	b.Grow(cap)

	err := binary.Write(b, binary.BigEndian, OpERR)
	if err != nil {
		return nil, err
	}

	err = binary.Write(b, binary.BigEndian, e.Error)
	if err != nil {
		return nil, err
	}

	_, err = b.WriteString(e.Message)
	if err != nil {
		return nil, err
	}

	err = b.WriteByte(0)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (e *Error) UnmarshalBinary(data []byte) error {
	var opCode OpCode
	r := bytes.NewBuffer(data)

	err := binary.Read(r, binary.BigEndian, &opCode)
	if err != nil {
		return err
	}

	if opCode != OpERR {
		return errors.New("invalid error")
	}

	err = binary.Read(r, binary.BigEndian, &e.Error)
	if err != nil {
		return errors.New("invalid error")
	}

	e.Message, err = r.ReadString(0)
	if err != nil {
		return errors.New("invalid error")
	}

	e.Message = strings.TrimRight(e.Message, "\x00")

	return nil
}

type Server struct {
	Timeout time.Duration
	Payload []byte
	Retries uint16
}

func (s *Server) ListenAndServe(addr string) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Println("Listening on ", conn.LocalAddr())

	return s.Serve(conn)

}

func (s *Server) Serve(conn net.PacketConn) error {

	if conn == nil {
		return errors.New("nil connection")
	}

	if s.Payload == nil {
		return errors.New("nil payload")
	}

	if s.Retries == 0 {
		s.Retries = 3
	}

	if s.Timeout == 0 {
		s.Timeout = 10 * time.Second
	}

	var rrq ReadReq

	for {

		buf := make([]byte, DatagramSize)

		_, addr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Println("error reading request: ", err)
			return err
		}

		err = rrq.UnmarshalBinary(buf)

		log.Println("received request: ", rrq)

		if err != nil {
			log.Printf("[%s] bad request: %v", addr.String(), err)
			continue
		}

		go s.handle(addr.String(), rrq)
	}
	return nil

}

func (s *Server) handle(addr string, rrq ReadReq) {

	log.Printf("[%s] request file: %s", addr, rrq.FileName)

	conn, err := net.DialTimeout("udp", addr, s.Timeout)
	if err != nil {
		log.Printf("[%s] error dialing: %v", addr, err)
		return
	}

	defer conn.Close()

	var (
		ackPkt  Ack
		errPkt  Error
		dataPkt = Data{Payload: bytes.NewBuffer(s.Payload)}
		buf     = make([]byte, DatagramSize)
	)

NEXTPACKET:
	for n := DatagramSize; n == DatagramSize; {
		data, err := dataPkt.MarshalBinary()

		if err != nil {
			log.Printf("[%s] error marshaling data: %v", addr, err)
			return
		}
	RETRY:
		for i := s.Retries; i > 0; i-- {
			n, err = conn.Write(data)

			if err != nil {
				log.Printf("[%s] error writing data: %v", addr, err)
				return
			}

			log.Printf("[%s] sent data block %d", addr, dataPkt.Block)

			err := conn.SetReadDeadline(time.Now().Add(s.Timeout))
			if err != nil {
				return
			}

			_, err = conn.Read(buf)
			if err != nil {
				if err, ok := err.(net.Error); ok && err.Timeout() {
					log.Printf("[%s] timeout, retrying", addr)
					continue RETRY
				}
				log.Printf("[%s] error reading ack: %v", addr, err)
				return
			}

			switch {
			case ackPkt.UnmarshalBinary(buf) == nil:
				if uint16(ackPkt) == dataPkt.Block {
					continue NEXTPACKET
				} else {
					log.Printf("[%s] invalid ack block: %d", addr, ackPkt)
					continue RETRY
				}
			case errPkt.UnmarshalBinary(buf) == nil:
				log.Printf("[%s] error: %s", addr, errPkt.Message)
				return
			default:
				log.Printf("[%s] invalid packet", addr)
			}

		}
		log.Printf("block %d exhausted retries", dataPkt.Block)

		return
	}

	log.Printf("[%s] send %d data blocks", addr, dataPkt.Block)

}
