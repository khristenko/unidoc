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

// MergePageWith appends page content to source Pdf file page content
func (a *PdfAppender) MergePageWith(pageNum int, page *PdfPage) error {
	page = page.Duplicate()
	pages, ok := a.pages.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return fmt.Errorf("ERROR: Pages not found in the source document")
	}
	kidsObj := pages.Get("Kids")
	kids, ok := kidsObj.(*core.PdfObjectArray)
	if !ok {
		return fmt.Errorf("ERROR: Kids not found in the source document")
	}
	srcPageObj := kids.Get(pageNum)
	if srcPageObj == nil {
		return fmt.Errorf("ERROR: Page %d not found in the source document", pageNum)
	}
	srcPageRef, ok := srcPageObj.(*core.PdfObjectReference)
	if !ok {
		return fmt.Errorf("ERROR: Page reference %d not found in the source document", pageNum)
	}
	srcPageObj, err := a.parser.LookupByNumber(int(srcPageRef.ObjectNumber))
	if err != nil {
		return err
	}
	srcPageInd, ok := srcPageObj.(*core.PdfIndirectObject)
	if !ok {
		return fmt.Errorf("ERROR: Page dictionary %d not found in the source document", pageNum)
	}
	srcPageDict, ok := srcPageInd.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return fmt.Errorf("ERROR: Page dictionary %d not found in the source document", pageNum)
	}

	newPage := core.MakeDict()
	newPage.Merge(srcPageDict)
	if parentRef, ok := newPage.Get("Parent").(*core.PdfObjectReference); ok {
		parent, err := a.parser.LookupByNumber(int(parentRef.ObjectNumber))
		if err != nil {
			return err
		}
		newPage.Set("Parent", parent)
	}

	srcContentsObj := srcPageDict.Get("Contents")
	if srcContentsObj == nil {
		return fmt.Errorf("ERROR: Page contents %s not found in the page", srcPageDict)
	}
	srcContents, ok := srcContentsObj.(*core.PdfObjectArray)
	if !ok {
		return fmt.Errorf("ERROR: Page contents %s not found in the page", srcPageDict)
	}

	contents, ok := page.Contents.(*core.PdfObjectArray)
	if !ok {
		contents = core.MakeArray(page.Contents)
		page.Contents = contents
	}

	elements := srcContents.Elements()
	elements = append(elements, contents.Elements()...)
	newPage.Set("Contents", core.MakeArray(elements...))

	obj := core.MakeIndirectObject(newPage)
	a.newObjects = append(a.newObjects, obj)
	kids.Set(pageNum, obj)
	return nil
}

// AddPages adds pages to end of the source Pdf
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
	for _, obj := range a.newObjects {
		w.addObject(obj)
	}

	for _, page := range a.newPages {
		w.AddPage(page)
	}
	return w.Write(fWrite)
}
