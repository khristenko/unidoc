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
	"strings"

	"github.com/unidoc/unidoc/common"
	"github.com/unidoc/unidoc/pdf/core"
)

// PdfAppender appends a new Pdf content to an existing Pdf document.
type PdfAppender struct {
	rs           io.ReadSeeker
	parser       *core.PdfParser
	reader       *PdfReader
	root         *core.PdfIndirectObject
	infoObj      *core.PdfIndirectObject
	catalog      *core.PdfObjectDictionary
	ppages       *core.PdfIndirectObject
	xrefs        core.XrefTable
	srcResources map[string]*core.PdfObjectDictionary

	newPages         []*PdfPage
	newObjects       []core.PdfObject
	lastObjectNumber int
	greatestObjNum   int
	pagesDict        map[int]*core.PdfObjectDictionary
	newPagesDict     map[int]*core.PdfIndirectObject
	kids             *core.PdfObjectArray

	//
	resourcesRenameMap map[string]string
}

// getPageResourcesByName returns a list of the page resources by name and contained PdfObjectDictionary.
func getPageResourcesByName(parser *core.PdfParser, page *core.PdfObjectDictionary, dest map[string]*core.PdfObjectDictionary) error {
	resources, err := getDict(traceObject(parser, page.Get("Resources")))
	if err != nil {
		return err
	}
	extGState, err := getDict(traceObject(parser, resources.Get("ExtGState")))
	if err != nil {
		return err
	}
	for _, key := range extGState.Keys() {
		dest[string(key)] = extGState
	}
	xObject, err := getDict(traceObject(parser, resources.Get("XObject")))
	if err != nil {
		return err
	}
	for _, key := range xObject.Keys() {
		dest[string(key)] = xObject
	}
	font, err := getDict(traceObject(parser, resources.Get("Font")))
	if err != nil {
		return err
	}
	for _, key := range font.Keys() {
		dest[string(key)] = font
	}
	return nil
}

// NewPdfAppender creates a new PdfAppender from PdfReader.
func NewPdfAppender(rs io.ReadSeeker) (*PdfAppender, error) {
	a := &PdfAppender{rs: rs}
	a.pagesDict = make(map[int]*core.PdfObjectDictionary)
	a.resourcesRenameMap = make(map[string]string)
	a.newPagesDict = make(map[int]*core.PdfIndirectObject)

	var err error
	a.parser, err = core.NewParser(rs)
	if err != nil {
		return nil, err
	}
	for _, idx := range a.parser.GetObjectNums() {
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
	a.ppages = ppages
	a.catalog.Set("Pages", ppages)
	a.xrefs = a.parser.GetXrefTable()

	pages, ok := ppages.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("ERROR: Pages not found in the source document")
	}
	kidsObj := pages.Get("Kids")
	a.kids, ok = kidsObj.(*core.PdfObjectArray)
	if !ok {
		return nil, fmt.Errorf("ERROR: Kids not found in the source document")
	}
	for i := 0; i < a.kids.Len(); i++ {
		srcPageObj := a.kids.Get(i)
		if srcPageObj == nil {
			return nil, fmt.Errorf("ERROR: Page %d "+
				"not found in the source document", i)
		}
		srcPageRef, ok := srcPageObj.(*core.PdfObjectReference)
		if !ok {
			return nil, fmt.Errorf("ERROR: Page reference %d not found in the source document", i)
		}
		srcPageObj, err := a.parser.LookupByNumber(int(srcPageRef.ObjectNumber))
		if err != nil {
			return nil, err
		}
		srcPageInd, ok := srcPageObj.(*core.PdfIndirectObject)
		if !ok {
			return nil, fmt.Errorf("ERROR: Page dictionary %d not found in the source document", i)
		}
		srcPageDict, ok := srcPageInd.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil, fmt.Errorf("ERROR: Page dictionary %d not found in the source document", i)
		}
		a.pagesDict[i] = srcPageDict
	}

	a.srcResources = make(map[string]*core.PdfObjectDictionary)
	for _, page := range a.pagesDict {
		getPageResourcesByName(a.parser, page, a.srcResources)
	}

	return a, nil
}

func (a *PdfAppender) addNewObject(obj core.PdfObject) {
	for _, o := range a.newObjects {
		if o == obj {
			return
		}
	}
	a.newObjects = append(a.newObjects, obj)
}

func (a *PdfAppender) renameResources(page *core.PdfObjectDictionary) error {
	contents, hasContents := core.GetArray(page.Get("Contents"))
	if !hasContents {
		return nil
	}

	pageResources := make(map[string]*core.PdfObjectDictionary)
	streamRenameMap := make(map[string]string)
	getPageResourcesByName(nil, page, pageResources)
	for name := range pageResources {
		if _, contained := a.srcResources[name]; contained {
			newName := name + "1"
			a.resourcesRenameMap[name] = newName
			streamRenameMap["/"+name] = "/" + newName
		}
	}

	for name, dict := range pageResources {
		if newName, hasNewName := a.resourcesRenameMap[name]; hasNewName {
			obj := dict.Get(core.PdfObjectName(name))
			dict.Set(core.PdfObjectName(newName), obj)
			dict.Remove(core.PdfObjectName(name))
			a.addNewObject(obj)
		}
	}

	for _, obj := range contents.Elements() {
		if stream, isStream := core.GetStream(obj); isStream {
			streamEncoder, err := core.NewEncoderFromStream(stream)
			if err != nil {
				return err
			}
			data, err := streamEncoder.DecodeStream(stream)
			if err != nil {
				return err
			}
			dataStr := string(data)
			for oldName, newName := range streamRenameMap {
				dataStr = strings.Replace(dataStr, oldName, newName, -1)
			}
			//stream, err = core.MakeStream([]byte(dataStr), streamEncoder)
			stream.Stream, err = streamEncoder.EncodeBytes([]byte(dataStr))
			if err != nil {
				return err
			}
			stream.PdfObjectDictionary.Set("Length", core.MakeInteger(int64(len(stream.Stream))))
			//contents.Set(index, stream)
		}
	}

	return nil
}

// MergePageWith appends page content to source Pdf file page content.
func (a *PdfAppender) MergePageWith(pageNum int, page *PdfPage) error {
	page = page.Duplicate()

	srcPageDict, ok := a.pagesDict[pageNum]
	if !ok {
		return fmt.Errorf("ERROR: Page dictionary %d not found in the source document", pageNum)
	}

	newPageInd, pageExisting := a.newPagesDict[pageNum]
	if !pageExisting {
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
		elements := srcContents.Elements()
		newPage.Set("Contents", core.MakeArray(elements...))
		newPageInd = core.MakeIndirectObject(newPage)
	}

	newPage := newPageInd.PdfObject.(*core.PdfObjectDictionary)

	srcContents, srcContentsFound := core.GetArray(newPage.Get("Contents"))
	if !srcContentsFound {
		return fmt.Errorf("ERROR something went wrong source page N%d contents not found", pageNum)
	}

	contents, ok := page.Contents.(*core.PdfObjectArray)
	if !ok {
		contents = core.MakeArray(page.Contents)
		page.Contents = contents
	}
	page.ToPdfObject()
	srcContents.Append(contents.Elements()...)
	for _, o := range contents.Elements() {
		a.addNewObject(o)
	}
	a.renameResources(page.pageDict)

	resourcesIndObj, err := traceObject(a.parser, newPage.Get("Resources"))
	if err != nil {
		return err
	}

	resourcesDict, ok := core.GetDict(resourcesIndObj)
	if !ok {
		return fmt.Errorf("ERROR Resources dictionary from %s object", resourcesIndObj.String())
	}

	if page.Resources.Font != nil {
		fontIndObj, err := traceObject(a.parser, resourcesDict.Get("Font"))
		if err != nil {
			return err
		}
		fontDict, ok := core.GetDict(fontIndObj)
		if !ok {
			return fmt.Errorf("ERROR get Font dictionary resource %s", fontIndObj.String())
		}
		dict, ok := core.GetDict(page.Resources.Font)
		if !ok {
			return fmt.Errorf("ERROR get Font dictionary from the page")
		}
		fontDict.Merge(dict)
		resourcesDict.Set("Font", fontIndObj)
		newPage.Set("Resources", resourcesIndObj)
		//a.addNewObject(resourcesIndObj)
		//a.addNewObject(fontIndObj)
	}

	if page.Resources.ExtGState != nil {
		extGStateIndObj, err := traceObject(a.parser, resourcesDict.Get("ExtGState"))
		if err != nil {
			return err
		}
		extGStateDict, ok := core.GetDict(extGStateIndObj)
		if !ok {
			return fmt.Errorf("ERROR get ExtGState dictionary resource %s", extGStateIndObj.String())
		}
		dict, ok := core.GetDict(page.Resources.ExtGState)
		if !ok {
			return fmt.Errorf("ERROR get ExtGState dictionary from the page")
		}
		extGStateDict.Merge(dict)
		resourcesDict.Set("ExtGState", extGStateIndObj)
		newPage.Set("Resources", resourcesIndObj)
		//a.addNewObject(resourcesIndObj)
		//a.addNewObject(extGStateIndObj)
	}

	if page.Resources.XObject != nil {
		xObjectIndObj, err := traceObject(a.parser, resourcesDict.Get("XObject"))
		if err != nil {
			return err
		}
		xObjectDict, ok := core.GetDict(xObjectIndObj)
		if !ok {
			return fmt.Errorf("ERROR get XObject dictionary resource %s", xObjectIndObj.String())
		}
		dict, ok := core.GetDict(page.Resources.XObject)
		if !ok {
			return fmt.Errorf("ERROR get XObject dictionary from the page")
		}
		xObjectDict.Merge(dict)
		resourcesDict.Set("XObject", xObjectIndObj)
		newPage.Set("Resources", resourcesIndObj)
		//a.addNewObject(resourcesIndObj)
		//a.addNewObject(xObjectIndObj)
	}

	// MediaBox
	a.newPagesDict[pageNum] = newPageInd
	a.addNewObject(newPageInd)
	a.kids.Set(pageNum, newPageInd)
	return nil
}

// AddPages adds pages to end of the source Pdf.
func (a *PdfAppender) AddPages(pages ...*PdfPage) {
	for _, page := range pages {
		pageCopy := page.Duplicate()
		a.renameResources(pageCopy.pageDict)
		a.newPages = append(a.newPages, pageCopy)
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
func (a *PdfAppender) WriteToFile(outputPath string) error {
	fWrite, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer fWrite.Close()
	if len(a.newPages) == 0 && len(a.newObjects) == 0 {
		_, err = a.write(fWrite)
		return err
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

func traceObject(parser *core.PdfParser, obj core.PdfObject) (core.PdfObject, error) {
	if ref, ok := obj.(*core.PdfObjectReference); ok && parser != nil {
		return parser.LookupByNumber(int(ref.ObjectNumber))
	}
	return obj, nil
}

func getArray(obj core.PdfObject, err error) (*core.PdfObjectArray, error) {
	if err != nil {
		return nil, err
	}
	arr, arrayFound := core.GetArray(obj)
	if !arrayFound {
		return nil, fmt.Errorf("ERROR obj is not Pdf array")
	}
	return arr, nil
}

func getDict(obj core.PdfObject, err error) (*core.PdfObjectDictionary, error) {
	if err != nil {
		return nil, err
	}
	dict, dictFound := core.GetDict(obj)
	if !dictFound {
		return nil, fmt.Errorf("ERROR obj is not Pdf dictionary")
	}
	return dict, nil
}
