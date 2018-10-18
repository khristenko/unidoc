/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package model

import (
	"os"
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

	appender.WriteFile("/tmp/appender_1.pdf")
}
