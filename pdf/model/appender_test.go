/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package model

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/unidoc/unidoc/common"
)

// This test file contains multiple tests to generate PDFs from existing Pdf files. The outputs are written into /tmp as files.  The files
// themselves need to be observed to check for correctness as we don't have a good way to automatically check
// if every detail is correct.

func init() {
	common.SetLogger(common.NewConsoleLogger(common.LogLevelDebug))
}

const testPdfFile1 = "./testdata/minimal.pdf"
const testPdfLoremIpsumFile = "./testdata/lorem.pdf"

// source http://foersom.com/net/HowTo/data/OoPdfFormExample.pdf
const testPdfAcroFormFile1 = "./testdata/OoPdfFormExample.pdf"

const imgPdfFile1 = "./testdata/img1-1.pdf"
const imgPdfFile2 = "./testdata/img1-2.pdf"

func tempFile(name string) string {
	return filepath.Join("/tmp" /*os.TempDir() */, name)
}

func TestAppender1(t *testing.T) {

	f1, err := os.Open(testPdfLoremIpsumFile)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f1.Close()
	pdf1, err := NewPdfReader(f1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	f2, err := os.Open(testPdfFile1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f2.Close()
	pdf2, err := NewPdfReader(f2)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender, err := NewPdfAppender(pdf1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	err = appender.MergePageWith(0, pdf2.PageList[0])
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender.AddPages(pdf2.PageList...)

	err = appender.WriteToFile(tempFile("appender_1.pdf"))
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
}

func TestAppender2(t *testing.T) {

	f1, err := os.Open(imgPdfFile1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f1.Close()

	pdf1, err := NewPdfReader(f1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	f2, err := os.Open(imgPdfFile2)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f2.Close()

	pdf2, err := NewPdfReader(f2)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender, err := NewPdfAppender(pdf1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	err = appender.MergePageWith(0, pdf2.PageList[0])
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender.AddPages(pdf2.PageList...)

	err = appender.WriteToFile(tempFile("appender_2.pdf"))
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
}

func TestAppender3(t *testing.T) {

	f1, err := os.Open(testPdfFile1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f1.Close()
	pdf1, err := NewPdfReader(f1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	f2, err := os.Open(testPdfLoremIpsumFile)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f2.Close()

	pdf2, err := NewPdfReader(f2)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender, err := NewPdfAppender(pdf1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	err = appender.MergePageWith(0, pdf2.PageList[0])
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender.AddPages(pdf2.PageList...)
	appender.AddPages(pdf1.PageList[0])

	err = appender.WriteToFile(tempFile("appender_3.pdf"))
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
}

func pemBlockForKey(priv interface{}) *pem.Block {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}
	case *ecdsa.PrivateKey:
		b, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to marshal ECDSA private key: %v", err)
			os.Exit(2)
		}
		return &pem.Block{Type: "EC PRIVATE KEY", Bytes: b}
	default:
		return nil
	}
}

func generateCertificate() error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	/*
		template := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject: pkix.Name{
				Organization: []string{"Acme Co"},
			},
			NotBefore: time.Now(),
			NotAfter:  time.Now().Add(time.Hour * 24 * 180),

			KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			BasicConstraintsValid: true,
		}

		derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
		if err != nil {
			return err
		}
		out := &bytes.Buffer{}
		pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
		fmt.Println(out.String())
		out.Reset()
		pem.Encode(out, pemBlockForKey(priv))
		fmt.Println(out.String())
	*/
	data := []byte("Test message")
	hash := sha1.New()
	hash.Write(data)

	hSum := hash.Sum(nil)
	signature, err := rsa.SignPSS(rand.Reader, priv, crypto.SHA1, hSum, nil)
	if err != nil {
		return err
	}
	fmt.Println(signature)
	err = rsa.VerifyPSS(&priv.PublicKey, crypto.SHA1, hSum, signature, nil)
	if err != nil {
		return err
	}
	return nil
}

func TestAppender4(t *testing.T) {
	f1, err := os.Open(testPdfAcroFormFile1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f1.Close()
	pdf1, err := NewPdfReader(f1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	f2, err := os.Open(imgPdfFile2)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer f2.Close()

	pdf2, err := NewPdfReader(f2)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender, err := NewPdfAppender(pdf1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	// appender.CreateSignatureField()

	generateCertificate()
	signatureField, wa := appender.CreateSignatureField()
	form := pdf1.AcroForm
	if form == nil {
		form = NewPdfAcroForm()
	}
	if form.Fields == nil {
		fields := make([]*PdfField, 0, 1)
		form.Fields = &fields
	}
	*form.Fields = append(*form.Fields, signatureField)
	rect := PdfRectangle{Llx: 20, Lly: 50, Urx: 10 + 50, Ury: 50 + 50}
	wa.Rect = rect.ToPdfObject()

	// Add to the page annotations.
	page := pdf1.PageList[0]
	wa.ToPdfObject()
	page.Annotations = append(page.Annotations, wa.PdfAnnotation)

	//appender.ReplacePage(0, page)
	//appender.ReplaceForm(form)
	appender.MergePageWith(0, pdf2.PageList[0])

	err = appender.WriteToFile(tempFile("appender_4.pdf"))
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
}
