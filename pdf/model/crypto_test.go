package model

import "testing"

const testCert = "./testdata/ks"

func TestCryptoLoad(t *testing.T) {
	err := (&CryptoPkcs12{}).Load(testCert)
	if err != nil {
		t.Error(err)
	}
}
