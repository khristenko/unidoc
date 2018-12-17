package model

import (
	"crypto/rsa"
	"crypto/x509"
	"io/ioutil"

	"golang.org/x/crypto/pkcs12"
)

type CryptoPkcs12 struct {
	PublicKey   []byte
	PrivateKey  []byte
	Certificate *x509.Certificate
}

func NewCrypto(fileName, password string) (*CryptoPkcs12, error) {
	c := &CryptoPkcs12{}
	return c, c.load(fileName, password)
}

func (c *CryptoPkcs12) load(fileName, password string) error {
	f, _ := ioutil.ReadFile(fileName)
	privateKey, cert, err := pkcs12.Decode(f, password)
	if err != nil {
		return err
	}
	c.PublicKey = cert.PublicKey.(*rsa.PublicKey).N.Bytes()
	c.PrivateKey = privateKey.(*rsa.PrivateKey).N.Bytes()
	c.Certificate = cert
	return nil
}

func (c *CryptoPkcs12) Encrypt() {

}

func (c *CryptoPkcs12) Decrypt() {

}
