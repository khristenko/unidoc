/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package model

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/unidoc/unidoc/common"
	"github.com/unidoc/unidoc/pdf/core"
)

// PdfAppender appends a new Pdf content to an existing Pdf document.
type PdfAppender struct {
	rs      io.ReadSeeker
	parser  *core.PdfParser
	root    *core.PdfIndirectObject
	infoObj *core.PdfIndirectObject
	catalog *core.PdfObjectDictionary
	pages   *core.PdfIndirectObject
	xrefs   core.XrefTable

	newPages         []*PdfPage
	newObjects       []core.PdfObject
	lastObjectNumber int
	greatestObjNum   int
}

// NewPdfAppender creates a new PdfAppender from PdfReader
func NewPdfAppender(rs io.ReadSeeker) (*PdfAppender, error) {
	a := &PdfAppender{rs: rs}
	var err error
	a.parser, err = core.NewParser(rs)
	if err != nil {
		return nil, err
	}
	for idx := range a.parser.GetObjectNums() {
		if a.greatestObjNum < idx {
			a.greatestObjNum = idx
		}
	}

	trailer := a.parser.GetTrailer()
	if trailer == nil {
		return nil, fmt.Errorf("Missing trailer")
	}
	// Catalog.
	root, ok := trailer.Get("Root").(*core.PdfObjectReference)
	if !ok {
		return nil, fmt.Errorf("Invalid Root (trailer: %s)", *trailer)
	}
	oc, err := a.parser.LookupByReference(*root)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read root element catalog: %s", err)
		return nil, err
	}
	a.root = oc.(*core.PdfIndirectObject)
	a.catalog, ok = a.root.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Missing catalog: (root %q) (trailer %s)", oc, *trailer)
		return nil, errors.New("Missing catalog")
	}

	// Pages.
	pagesRef, ok := a.catalog.Get("Pages").(*core.PdfObjectReference)
	if !ok {
		return nil, errors.New("Pages in catalog should be a reference")
	}
	op, err := a.parser.LookupByReference(*pagesRef)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read pages")
		return nil, err
	}
	ppages, ok := op.(*core.PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid")
		common.Log.Debug("op: %p", ppages)
		return nil, errors.New("Pages object invalid")
	}
	a.pages = ppages
	a.catalog.Set("Pages", ppages)
	a.xrefs = a.parser.GetXrefTable()
	return a, nil
}

func (a *PdfAppender) AddPages(pages ...*PdfPage) {
	for _, page := range pages {
		a.newPages = append(a.newPages, page)
	}
	return
}

func (a *PdfAppender) write(w io.Writer) (int64, error) {
	if _, err := a.rs.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	return io.Copy(w, a.rs)
}

// WriteToFile writes the Appender output to file specified by path.
func (a *PdfAppender) WriteFile(outputPath string) error {
	fWrite, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer fWrite.Close()
	if len(a.newPages) == 0 && len(a.newObjects) == 0 {
		return nil
	}

	w := newPdfWriterFromAppender(a)
	for _, page := range a.newPages {
		w.AddPage(page)
	}
	return w.Write(fWrite)
}
