/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package model

import (
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

const imgPdfFile1 = "./testdata/img1-1.pdf"
const imgPdfFile2 = "./testdata/img1-2.pdf"

func tempFile(name string) string {
	return filepath.Join("/tmp" /*os.TempDir() */, name)
}

func TestAppender1(t *testing.T) {

	sourceFile, err := os.Open(testPdfLoremIpsumFile)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer sourceFile.Close()

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

	appender, err := NewPdfAppender(sourceFile)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender.MergePageWith(0, pdf2.PageList[0])

	appender.AddPages(pdf2.PageList...)

	appender.WriteToFile(tempFile("appender_1.pdf"))
}

func TestAppender2(t *testing.T) {

	sourceFile, err := os.Open(imgPdfFile1)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
	defer sourceFile.Close()

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

	appender, err := NewPdfAppender(sourceFile)
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}

	appender.MergePageWith(0, pdf2.PageList[0])

	appender.AddPages(pdf2.PageList...)

	err = appender.WriteToFile(tempFile("appender_2.pdf"))
	if err != nil {
		t.Errorf("Fail: %v\n", err)
		return
	}
}
