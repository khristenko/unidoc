package model

import (
	"testing"
)

const signedFile = "./testdata/SampleSignedPDFDocument.pdf"

func TestNewPdfSignature(t *testing.T) {
	//f1, err := os.Open(signedFile)
	//if err != nil {
	//	t.Errorf("failed open file %v", err)
	//}
	//defer func() {
	//	err := f1.Close()
	//	if err != nil {
	//		t.Errorf("failed close file %v", err)
	//	}
	//}()
	//pdf1, err := NewPdfReader(f1)
	//if err != nil {
	//	t.Errorf("Fail: %v\n", err)
	//	return
	//}
	//dict := pdf1.parser.ParseDict()
	//fmt.Printf("%v", dict)
}
