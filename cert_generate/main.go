package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

var (
	host = flag.String("host", "localhost", "")
	cert = flag.String("cert", "cert.pem", "")
	key  = flag.String("key", "key.pem", "")
)

func main() {
	flag.Parse()

	err := GenerateCert(*host, *cert, *key)

	if err != nil {
		panic(err)
	}
}

func GenerateCert(host string, certFn string, keyFn string) error {

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	if err != nil {
		return err
	}

	notBefore := time.Now()

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"Acme Co"},
		},
		NotBefore: notBefore,
		NotAfter:  notBefore.Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	for _, h := range strings.Split(host, ",") {
		ip := net.ParseIP(h)
		if ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	if err != nil {
		log.Println(err)
	}

	certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)

	if err != nil {
		log.Println(err)
	}

	certOut, err := os.Create(certFn)

	err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certificate})

	if err != nil {
		log.Println(err)
	}

	err = certOut.Close()
	if err != nil {
		log.Println(err)
	}

	log.Println("wrote", certFn)

	key, err := os.OpenFile(keyFn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)

	if err != nil {
		log.Println(err)
	}

	priKey, err := x509.MarshalECPrivateKey(privateKey)

	err = pem.Encode(key, &pem.Block{Bytes: priKey, Type: "EC PRIVATE KEY"})

	if err != nil {
		log.Println(err)
	}

	err = key.Close()
	if err != nil {
		log.Println(err)
	}

	log.Println("wrote", keyFn)

	return nil
}
