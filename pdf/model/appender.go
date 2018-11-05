/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package model

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/unidoc/unidoc/common"
	"github.com/unidoc/unidoc/pdf/core"
)

// PdfAppender appends a new Pdf content to an existing Pdf document.
type PdfAppender struct {
	rs     io.ReadSeeker
	parser *core.PdfParser
	Reader *PdfReader

	root    *core.PdfIndirectObject
	catalog *core.PdfObjectDictionary
	ppages  *core.PdfIndirectObject
	pages   *core.PdfObjectDictionary

	xrefs              core.XrefTable
	srcResources       map[string]struct{}
	srcIndirectObjects map[core.PdfObject]core.PdfObjectReference

	newObjects       []core.PdfObject
	hasNewObject     map[core.PdfObject]struct{}
	lastObjectNumber int
	greatestObjNum   int
	pagesDict        map[int]*core.PdfObjectDictionary
	newPagesDict     map[int]*core.PdfIndirectObject
	kids             *core.PdfObjectArray
	defaultMediaBox  *PdfRectangle
	//
	resourcesRenameMap    map[string]string
	objectToObjectCopyMap map[core.PdfObject]core.PdfObject
	acroForm              *PdfAcroForm
	signatureDict         *pdfSignDictionary
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

func NewPdfAppender(reader *PdfReader) (*PdfAppender, error) {
	a := &PdfAppender{}
	a.pagesDict = make(map[int]*core.PdfObjectDictionary)
	a.resourcesRenameMap = make(map[string]string)
	a.newPagesDict = make(map[int]*core.PdfIndirectObject)
	a.hasNewObject = make(map[core.PdfObject]struct{})
	a.srcResources = make(map[string]struct{})
	a.srcIndirectObjects = make(map[core.PdfObject]core.PdfObjectReference)
	a.objectToObjectCopyMap = make(map[core.PdfObject]core.PdfObject)
	a.acroForm = reader.AcroForm
	a.rs = reader.rs
	a.Reader = reader
	a.parser = a.Reader.parser

	for _, idx := range a.Reader.GetObjectNums() {
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
		return nil, fmt.Errorf("Invalid Root (trailer: %s)", trailer)
	}
	oc, err := a.parser.LookupByReference(*root)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read root element catalog: %s", err)
		return nil, err
	}
	a.root = oc.(*core.PdfIndirectObject)
	a.catalog, ok = a.root.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Missing catalog: (root %q) (trailer %s)", oc, trailer)
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

	if pages, ok := ppages.PdfObject.(*core.PdfObjectDictionary); ok {
		a.pages = pages
		if mediaBox, found := core.GetArray(pages.Get("MediaBox")); found {
			mb, err := NewPdfRectangle(*mediaBox)
			if err != nil {
				return nil, err
			}
			a.defaultMediaBox = mb
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
			if srcPageRef, ok := srcPageObj.(*core.PdfObjectReference); ok {
				srcPageObj, err = a.parser.LookupByNumber(int(srcPageRef.ObjectNumber))
				if err != nil {
					return nil, err
				}
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
	}

	setResourceNames := func(res core.PdfObject) {
		if res == nil {
			return
		}
		dict, ok := core.GetDict(res)
		if !ok {
			return
		}
		for _, key := range dict.Keys() {
			a.srcResources[string(key)] = struct{}{}
		}
	}

	for _, page := range a.Reader.PageList {
		if page.Resources == nil {
			continue
		}
		setResourceNames(page.Resources.XObject)
		setResourceNames(page.Resources.ExtGState)
		setResourceNames(page.Resources.Font)
	}

	a.lookupIndirectObjects(a.root)
	return a, nil
}

func (a *PdfAppender) lookupIndirectObjects(obj core.PdfObject) {
	if _, ok := a.srcIndirectObjects[obj]; ok {
		return
	}
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		a.srcIndirectObjects[obj] = v.PdfObjectReference
		a.lookupIndirectObjects(v.PdfObject)
	case *core.PdfObjectArray:
		for _, o := range v.Elements() {
			a.lookupIndirectObjects(o)
		}
	case *core.PdfObjectDictionary:
		for _, key := range v.Keys() {
			a.lookupIndirectObjects(v.Get(key))
		}
	case *core.PdfObjectStreams:
		for _, o := range v.Elements() {
			a.lookupIndirectObjects(o)
		}
	case *core.PdfObjectStream:
		a.srcIndirectObjects[obj] = v.PdfObjectReference
	}
}

func (a *PdfAppender) addNewObjects(obj core.PdfObject) {
	if _, ok := a.hasNewObject[obj]; ok || obj == nil {
		return
	}
	if _, ok := a.srcIndirectObjects[obj]; ok {
		return
	}

	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		a.newObjects = append(a.newObjects, obj)
		a.hasNewObject[obj] = struct{}{}
		a.addNewObjects(v.PdfObject)
	case *core.PdfObjectArray:
		for _, o := range v.Elements() {
			a.addNewObjects(o)
		}
	case *core.PdfObjectDictionary:
		for _, key := range v.Keys() {
			a.addNewObjects(v.Get(key))
		}
	case *core.PdfObjectStreams:
		for _, o := range v.Elements() {
			a.addNewObjects(o)
		}
	case *core.PdfObjectStream:
		a.newObjects = append(a.newObjects, obj)
		a.hasNewObject[obj] = struct{}{}
		a.addNewObjects(v.PdfObjectDictionary)
	}
}

// getNewName returns a new unique name from the given name
func (a *PdfAppender) getNewName(name string) string {
	for i := 1; true; i++ {
		newName := name + strconv.Itoa(i)
		if _, exists := a.srcResources[newName]; !exists {
			return newName
		}
	}
	panic("Never happen getNewName")
	return ""
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
			newName := a.getNewName(name)
			a.resourcesRenameMap[name] = newName
			streamRenameMap["/"+name] = "/" + newName
		}
	}

	for name, dict := range pageResources {
		if newName, hasNewName := a.resourcesRenameMap[name]; hasNewName {
			obj := dict.Get(core.PdfObjectName(name))
			dict.Set(core.PdfObjectName(newName), obj)
			dict.Remove(core.PdfObjectName(name))
			a.addNewObjects(obj)
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
			stream.Stream, err = streamEncoder.EncodeBytes([]byte(dataStr))
			if err != nil {
				return err
			}
			stream.PdfObjectDictionary.Set("Length", core.MakeInteger(int64(len(stream.Stream))))
		}
	}

	return nil
}

func (a *PdfAppender) makeSignDict() *pdfSignDictionary {
	signValue := &pdfSignDictionary{PdfObjectDictionary: *core.MakeDict()}
	signValue.Set("Type", core.MakeName("Sig"))
	signValue.Set("Filter", core.MakeName("Adobe.PPKLite"))
	signValue.Set("SubFilter", core.MakeName("adbe.x509.rsa_sha1"))
	reference := core.MakeDict()
	reference.Set("Type", core.MakeName("SigRef"))
	reference.Set("TransformMethod", core.MakeName("FieldMDP"))
	//reference.Set("Data", ) // ref to Catalog
	reference.Set("DigestMethod", core.MakeName("SHA1"))
	params := core.MakeDict()
	params.Set("Type", core.MakeName("TransformParams"))
	params.Set("Action", core.MakeName("All"))
	params.Set("V", core.MakeName("1.2"))
	reference.Set("TransformParams", params)
	signValue.Set("Reference", reference)
	//core.MakeStringFromBytes()
	// key
	signValue.Set("Contents", core.MakeHexString("ff"))
	return signValue
}

// CreateSignatureField creates a digital
func (a *PdfAppender) CreateSignatureField() (*PdfField, *PdfAnnotationWidget) {
	f := NewPdfField()
	signValue := a.makeSignDict()
	f.V = core.MakeIndirectObject(signValue) // makeSignatureValue
	f.FT = core.MakeName("Sig")
	f.T = core.MakeString("Signature1") // check number

	annot := NewPdfAnnotationWidget()
	//annot.
	return f, annot
}

func (a *PdfAppender) copyPage(page *PdfPage) (*core.PdfIndirectObject, *core.PdfObjectDictionary) {
	pageObj := page.ToPdfObject()
	pageIndirect := pageObj.(*core.PdfIndirectObject)
	pageDict := pageIndirect.PdfObject.(*core.PdfObjectDictionary)
	copyPageDict := core.MakeDict()

	for _, key := range pageDict.Keys() {
		obj := pageDict.Get(key)
		switch v := obj.(type) {
		case *core.PdfObjectArray:
			obj = core.MakeArray(v.Elements()...)
		case *core.PdfObjectDictionary:
			dict := core.MakeDict()
			dict.Merge(v)
			obj = dict
		case *core.PdfObjectStreams:
			obj = core.MakeObjectStreams(v.Elements()...)
		}
		copyPageDict.Set(key, obj)
	}
	copyPageDict = copyObject(copyPageDict, a.objectToObjectCopyMap).(*core.PdfObjectDictionary)
	copyPageIndirect := core.MakeIndirectObject(copyPageDict)
	return copyPageIndirect, copyPageDict
}

func (a *PdfAppender) ReplacePage(pageNum int, page *PdfPage) error {
	_, ok := a.pagesDict[pageNum]
	if !ok {
		return fmt.Errorf("ERROR: Page dictionary %d not found in the source document", pageNum)
	}
	pageIndirect, pageDict := a.copyPage(page)
	pageDict.Set("Parent", a.ppages)
	a.renameResources(pageDict)

	a.pagesDict[pageNum] = pageDict
	a.addNewObjects(pageIndirect)
	a.kids.Set(pageNum, pageIndirect)
	return nil
}

func (a *PdfAppender) RemovePage(pageNum int) error {
	/*
		_, ok := a.pagesDict[pageNum]
		if !ok {
			return fmt.Errorf("ERROR: Page dictionary %d not found in the source document", pageNum)
		}

		page.ToPdfObject()
		a.pagesDict[pageNum] = page.pageDict
		ind := page.GetPageAsIndirectObject()
		a.addNewObjects(ind)
		a.kids.Set(pageNum, ind)
		return nil
	*/
	return fmt.Errorf("RemovePage unimplementd")
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
		if mediaBox := newPage.Get("MediaBox"); mediaBox == nil {
			mediaBox = a.ppages
		}
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
		var elements []core.PdfObject
		switch srcContents := srcContentsObj.(type) {
		case *core.PdfObjectArray:
			elements = srcContents.Elements()
		default:
			elements = []core.PdfObject{srcContents}
		}
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
		a.addNewObjects(o)
	}
	a.renameResources(page.pageDict)

	resourcesIndObj, err := traceObject(a.parser, newPage.Get("Resources"))
	if err != nil {
		return err
	}

	if _, ok := a.srcIndirectObjects[resourcesIndObj]; ok {
		resourcesIndObj = core.MakeIndirectObject(resourcesIndObj.(*core.PdfIndirectObject).PdfObject)
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
	}

	if page.Resources.ExtGState != nil {
		extGStateIndObj, err := traceObject(a.parser, resourcesDict.Get("ExtGState"))
		if err != nil {
			return err
		}
		if extGStateDict, ok := core.GetDict(extGStateIndObj); ok {
			dict, ok := core.GetDict(page.Resources.ExtGState)
			if !ok {
				return fmt.Errorf("ERROR get ExtGState dictionary from the page")
			}
			extGStateDict.Merge(dict)
			resourcesDict.Set("ExtGState", extGStateIndObj)
			newPage.Set("Resources", resourcesIndObj)
		}
	}

	if page.Resources.XObject != nil {
		xObjectIndObj, err := traceObject(a.parser, resourcesDict.Get("XObject"))
		if err != nil {
			return err
		}
		if xObjectIndObj == nil {
			xObjectIndObj = core.MakeIndirectObject(core.MakeDict())
			a.addNewObjects(xObjectIndObj)
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
	}
	var mediaBox PdfRectangle
	if a.defaultMediaBox != nil {
		mediaBox = *a.defaultMediaBox
	}
	if mediaBoxArr, hasMediaBox := core.GetArray(newPage.Get("MediaBox")); hasMediaBox {
		mb, err := NewPdfRectangle(*mediaBoxArr)
		if err != nil {
			return err
		}
		mediaBox = *mb
	}
	var mediaBoxChanged bool
	if page.MediaBox != nil {
		if mediaBox.Llx > page.MediaBox.Llx {
			mediaBox.Llx = page.MediaBox.Llx
			mediaBoxChanged = true
		}
		if mediaBox.Lly > page.MediaBox.Lly {
			mediaBox.Lly = page.MediaBox.Lly
			mediaBoxChanged = true
		}
		if mediaBox.Urx < page.MediaBox.Urx {
			mediaBox.Urx = page.MediaBox.Urx
			mediaBoxChanged = true
		}
		if mediaBox.Ury < page.MediaBox.Ury {
			mediaBox.Ury = page.MediaBox.Ury
			mediaBoxChanged = true
		}
	}
	if mediaBoxChanged {
		newPage.Set("MediaBox", mediaBox.ToPdfObject())
	}
	a.newPagesDict[pageNum] = newPageInd
	a.addNewObjects(newPageInd)
	a.kids.Set(pageNum, newPageInd)
	return nil
}

// AddPages adds pages to end of the source Pdf.
func (a *PdfAppender) AddPages(pages ...*PdfPage) {
	for _, page := range pages {
		pageIndirect, pageDict := a.copyPage(page)
		//copyPageObj := copyObject(page.ToPdfObject(), a.objectToObjectCopyMap)
		//pageIndirect := copyPageObj.(*core.PdfIndirectObject)
		//pageDict := pageIndirect.PdfObject.(*core.PdfObjectDictionary)
		pageDict.Set("Parent", a.ppages)
		//pageCopy := page.Duplicate()
		a.renameResources(pageDict)
		index := len(a.pagesDict)
		//pageCopy.ToPdfObject()
		//pageDict := copyObject(pageCopy.pageDict, a.objectToObjectCopyMap).(*core.PdfObjectDictionary)
		a.pagesDict[index] = pageDict
		a.newPagesDict[index] = pageIndirect
		a.addNewObjects(pageIndirect)
		a.kids.Append(pageIndirect)
		a.pages.Set("Count", core.MakeInteger(int64(index+1)))
		//a.newPages = append(a.newPages, pageCopy)
	}
	return
}

// pdfSignDictionary is needed because of the digital checksum calculates after a new file creation and writes as a value into PdfDictionary in the existing file.
type pdfSignDictionary struct {
	core.PdfObjectDictionary
	fileOffset      int64
	contentsOffset  int
	byteRangeOffset int
}

// DefaultWriteString outputs the object as it is to be written to file.
func (d *pdfSignDictionary) DefaultWriteString() string {
	d.contentsOffset = 0
	outStr := "<<"
	for _, k := range d.Keys() {
		v := d.Get(k)
		switch k {
		case "ByteRange":
			outStr += k.DefaultWriteString()
			outStr += "                       "
			d.byteRangeOffset = len(outStr)
			outStr += v.DefaultWriteString()
		case "Contents":
			outStr += k.DefaultWriteString()
			outStr += " "
			d.contentsOffset = len(outStr)
			outStr += v.DefaultWriteString()
		default:
			outStr += k.DefaultWriteString()
			outStr += " "
			outStr += v.DefaultWriteString()
		}
	}
	outStr += ">>"
	return outStr
}

func (a *PdfAppender) ReplaceForm(form *PdfAcroForm) {
	obj := copyObject(form.ToPdfObject(), a.objectToObjectCopyMap)
	a.catalog.Set("AcroForm", obj)
	a.addNewObjects(obj)
}

// WriteToFile writes the Appender output to file specified by path.
func (a *PdfAppender) WriteToFile(outputPath string) error {
	fWrite, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer fWrite.Close()

	if _, err := a.rs.Seek(0, io.SeekStart); err != nil {
		return err
	}

	hashSha1 := sha1.New() // if needed
	reader := io.TeeReader(a.rs, hashSha1)
	offset, err := io.Copy(fWrite, reader)
	if err != nil {
		return err
	}
	if len(a.newObjects) == 0 {
		return nil
	}
	/*
		sd := &pdfSignDictionary{}
		sdInd := core.MakeIndirectObject(sd)
		a.newObjects = append(a.newObjects, sdInd)
	*/
	w := newPdfWriterFromAppender(a)
	w.writeOffset = offset
	w.ObjNumOffset = a.greatestObjNum
	w.appendMode = true
	w.appendToXrefs = a.xrefs
	for _, obj := range a.newObjects {
		w.addObject(obj)
	}
	//w.SetForms(a.acroForm)
	buffer := bytes.NewBuffer(nil)
	if err := w.Write(buffer); err != nil {
		return err
	}
	_, err = io.Copy(fWrite, buffer)
	//fmt.Printf("%x", sd.fileOffset)
	return err
}

func traceObject(parser *core.PdfParser, obj core.PdfObject) (core.PdfObject, error) {
	if ref, ok := obj.(*core.PdfObjectReference); ok && parser != nil {
		return parser.LookupByNumber(int(ref.ObjectNumber))
	}
	return obj, nil
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

// newPdfWriterFromAppender initializes a new PdfWriter from existing PdfAppender.
func newPdfWriterFromAppender(appender *PdfAppender) PdfWriter {
	w := NewPdfWriter()

	w.objectsMap = map[core.PdfObject]bool{}
	w.objects = []core.PdfObject{}
	w.pendingObjects = map[core.PdfObject]*core.PdfObjectDictionary{}

	// PDF Version.  Can be changed if using more advanced features in PDF.
	// By default it is set to 1.3.
	w.majorVersion = 1
	w.minorVersion = 3

	infoDict := core.MakeDict()
	infoDict.Set("Producer", core.MakeString(getPdfProducer()))
	infoDict.Set("Creator", core.MakeString(getPdfCreator()))
	w.infoObj = core.MakeIndirectObject(infoDict)
	w.addObject(w.infoObj)

	w.root = appender.root
	w.addObject(w.root)

	w.pages = appender.ppages
	w.addObject(w.pages)
	//appender.kids

	w.catalog = appender.catalog

	common.Log.Trace("Catalog %s", w.catalog)

	return w
}
