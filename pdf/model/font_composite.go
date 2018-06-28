package model

import (
	"errors"
	"io/ioutil"
	"sort"

	"github.com/unidoc/unidoc/common"
	. "github.com/unidoc/unidoc/pdf/core"
	"github.com/unidoc/unidoc/pdf/model/fonts"
	"github.com/unidoc/unidoc/pdf/model/textencoding"
)

/*
   9.7.2 CID-Keyed Fonts Overview (page 267)
   The CID-keyed font architecture specifies the external representation of certain font programs,
   called *CMap* and *CIDFont* files, along with some conventions for combining and using those files.

   A *CMap* (character map) file shall specify the correspondence between character codes and the CID
   numbers used to identify glyphs. It is equivalent to the concept of an encoding in simple fonts.
   Whereas a simple font allows a maximum of 256 glyphs to be encoded and accessible at one time, a
   CMap can describe a mapping from multiple-byte codes to thousands of glyphs in a large CID-keyed
   font.

   9.7.4 CIDFonts (page 269)

   A CIDFont program contains glyph descriptions that are accessed using a CID as the character
   selector. There are two types of CIDFonts:
   • A Type 0 CIDFont contains glyph descriptions based on CFF
   • A Type 2 CIDFont contains glyph descriptions based on the TrueType font format

   A CIDFont dictionary is a PDF object that contains information about a CIDFont program. Although
   its Type value is Font, a CIDFont is not actually a font.
       It does not have an Encoding entry,
       it may not be listed in the Font subdictionary of a resource dictionary, and
       it may not be used as the operand of the Tf operator.
       It shall be used only as a descendant of a Type 0 font.
   The CMap in the Type 0 font shall be what defines the encoding that maps character codes to CIDs
   in  the CIDFont.

    9.7.6 Type 0 Font Dictionaries (page 279)

    Type      Font
    Subtype   Type0
    BaseFont  (Required) The name of the font. If the descendant is a Type 0 CIDFont, this name
              should be the concatenation of the CIDFont’s BaseFont name, a hyphen, and the CMap
              name given in the Encoding entry (or the CMapName entry in the CMap). If the
              descendant is a Type 2 CIDFont, this name should be the same as the CIDFont’s BaseFont
              name.
              NOTE In principle, this is an arbitrary name, since there is no font program
                   associated directly with a Type 0 font dictionary. The conventions described here
                   ensure maximum compatibility with existing readers.
    Encoding name or stream (Required)
             The name of a predefined CMap, or a stream containing a CMap that maps character codes
             to font numbers and CIDs. If the descendant is a Type 2 CIDFont whose associated
             TrueType font program is not embedded in the PDF file, the Encoding entry shall be a
             predefined CMap name (see 9.7.4.2, "Glyph Selection in CIDFonts").

    Type 0 font from 000046.pdf

    103 0 obj
    << /Type /Font /Subtype /Type0 /Encoding /Identity-H /DescendantFonts [179 0 R]
    /BaseFont /FLDOLC+PingFangSC-Regular >>
    endobj
    179 0 obj
    << /Type /Font /Subtype /CIDFontType0 /BaseFont /FLDOLC+PingFangSC-Regular
    /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>
    /W 180 0 R /DW 1000 /FontDescriptor 181 0 R >>
    endobj
    180 0 obj
    [ ]
    endobj
    181 0 obj
    << /Type /FontDescriptor /FontName /FLDOLC+PingFangSC-Regular /Flags 4 /FontBBox
    [-123 -263 1177 1003] /ItalicAngle 0 /Ascent 972 /Descent -232 /CapHeight
    864 /StemV 70 /XHeight 648 /StemH 64 /AvgWidth 1000 /MaxWidth 1300 /FontFile3
    182 0 R >>
    endobj
    182 0 obj
    << /Length 183 0 R /Subtype /CIDFontType0C /Filter /FlateDecode >>
    stream
    ....
*/

// pdfFontType0 represents a Type0 font in PDF. Used for composite fonts which can encode multiple
// bytes for complex symbols (e.g. used in Asian languages). Represents the root font whereas the
// associated CIDFont is called its descendant.
type pdfFontType0 struct {
	container *PdfIndirectObject
	skeleton  *fontSkeleton

	encoder        textencoding.TextEncoder
	Encoding       PdfObject
	DescendantFont *PdfFont // Can be either CIDFontType0 or CIDFontType2 font.
}

// GetGlyphCharMetrics returns the character metrics for the specified glyph.  A bool flag is
// returned to indicate whether or not the entry was found in the glyph to charcode mapping.
func (font pdfFontType0) GetGlyphCharMetrics(glyph string) (fonts.CharMetrics, bool) {
	if font.DescendantFont == nil {
		common.Log.Debug("ERROR: No descendant. font=%s", font)
		return fonts.CharMetrics{}, false
	}
	return font.DescendantFont.GetGlyphCharMetrics(glyph)
}

// Encoder returns the font's text encoder.
func (font pdfFontType0) Encoder() textencoding.TextEncoder {
	return font.encoder
}

// SetEncoder sets the encoder for the truetype font.
func (font pdfFontType0) SetEncoder(encoder textencoding.TextEncoder) {
	font.encoder = encoder
}

// ToPdfObject converts the pdfFontType0 to a PDF representation.
func (font *pdfFontType0) ToPdfObject() PdfObject {
	if font.container == nil {
		font.container = &PdfIndirectObject{}
	}
	d := font.skeleton.toDict("Type0")
	font.container.PdfObject = d

	if font.Encoding != nil {
		d.Set("Encoding", font.Encoding)
	}
	if font.DescendantFont != nil {
		// Shall be 1 element array.
		d.Set("DescendantFonts", MakeArray(font.DescendantFont.ToPdfObject()))
	}

	return font.container
}

// newPdfFontType0FromPdfObject makes a pdfFontType0 based on the input PdfObject which should be
// represented by a dictionary. If a problem is encountered, an error is returned.
func newPdfFontType0FromPdfObject(obj PdfObject, skeleton *fontSkeleton) (*pdfFontType0, error) {

	d := skeleton.dict

	// DescendantFonts.
	arr, err := GetArray(TraceToDirectObject(d.Get("DescendantFonts")))
	if err != nil {
		common.Log.Debug("ERROR: Invalid DescendantFonts - not an array (%T) %s", obj, skeleton)
		return nil, ErrRangeError
	}
	if len(arr) != 1 {
		common.Log.Debug("ERROR: Array length != 1 (%d)", len(arr))
		return nil, ErrRangeError
	}
	df, err := newPdfFontFromPdfObject(arr[0], false)
	if err != nil {
		common.Log.Debug("ERROR: Failed loading descendant font: err=%v %s", err, skeleton)
		return nil, err
	}

	font := &pdfFontType0{
		skeleton:       skeleton,
		DescendantFont: df,
	}

	encoderName, err := GetName(TraceToDirectObject(d.Get("Encoding")))
	if err == nil && encoderName == "Identity-H" {
		font.encoder = textencoding.NewIdentityTextEncoder(encoderName)
	}
	return font, nil
}

// pdfCIDFontType0 represents a CIDFont Type0 font dictionary.
type pdfCIDFontType0 struct {
	container *PdfIndirectObject
	skeleton  *fontSkeleton // Elements common to all font types.

	encoder textencoding.TextEncoder

	// Table 117 – Entries in a CIDFont dictionary (page 269)
	CIDSystemInfo  PdfObject // (Required) Dictionary that defines the character collection of the CIDFont. See Table 116.
	FontDescriptor PdfObject // (Required) Describes the CIDFont’s default metrics other than its glyph widths
	DW             PdfObject // (Optional) Default width for glyphs in the CIDFont Default value: 1000 (defined in user units)
	W              PdfObject // (Optional) Widths for the glyphs in the CIDFont. Default value: none (the DW value shall be used for all glyphs).
	// DW2, W2: (Optional; applies only to CIDFonts used for vertical writing)
	DW2 PdfObject // An array of two numbers specifying the default metrics for vertical writing. Default value: [880 −1000].
	W2  PdfObject // A description of the metrics for vertical writing for the glyphs in the CIDFont. Default value: none (the DW2 value shall be used for all glyphs).

	// Also mapping from GIDs (glyph index) to widths.
	gidToWidthMap map[uint16]int
}

// Encoder returns the font's text encoder.
func (font pdfCIDFontType0) Encoder() textencoding.TextEncoder {
	return font.encoder
}

// SetEncoder sets the encoder for the truetype font.
func (font pdfCIDFontType0) SetEncoder(encoder textencoding.TextEncoder) {
	font.encoder = encoder
}

// GetGlyphCharMetrics returns the character metrics for the specified glyph.  A bool flag is
// returned to indicate whether or not the entry was found in the glyph to charcode mapping.
func (font pdfCIDFontType0) GetGlyphCharMetrics(glyph string) (fonts.CharMetrics, bool) {
	metrics := fonts.CharMetrics{}
	// Not implemented yet. !@#$
	return metrics, true
}

// ToPdfObject converts the pdfCIDFontType0 to a PDF representation.
func (font *pdfCIDFontType0) ToPdfObject() PdfObject {
	if font.container == nil {
		font.container = &PdfIndirectObject{}
	}
	d := font.skeleton.toDict("CIDFontType0")
	font.container.PdfObject = d

	if font.CIDSystemInfo != nil {
		d.Set("CIDSystemInfo", font.CIDSystemInfo)
	}
	if font.DW != nil {
		d.Set("DW", font.DW)
	}
	if font.DW2 != nil {
		d.Set("DW2", font.DW2)
	}
	if font.W != nil {
		d.Set("W", font.W)
	}
	if font.W2 != nil {
		d.Set("W2", font.W2)
	}

	return font.container
}

// newPdfCIDFontType0FromPdfObject creates a pdfCIDFontType0 object from a dictionary (either direct
// or via indirect object). If a problem occurs with loading an error is returned.
func newPdfCIDFontType0FromPdfObject(obj PdfObject, skeleton *fontSkeleton) (*pdfCIDFontType0, error) {
	if skeleton.subtype != "CIDFontType0" {
		common.Log.Debug("ERROR: Font SubType != CIDFontType0. font=%s", skeleton)
		return nil, ErrRangeError
	}

	font := &pdfCIDFontType0{skeleton: skeleton}
	d := skeleton.dict

	// CIDSystemInfo.
	obj = TraceToDirectObject(d.Get("CIDSystemInfo"))
	if obj == nil {
		common.Log.Debug("ERROR: CIDSystemInfo (Required) missing. font=%s", skeleton)
		return nil, ErrRequiredAttributeMissing
	}
	font.CIDSystemInfo = obj

	// Optional attributes.
	font.DW = TraceToDirectObject(d.Get("DW"))
	font.W = TraceToDirectObject(d.Get("W"))
	font.DW2 = TraceToDirectObject(d.Get("DW2"))
	font.W2 = TraceToDirectObject(d.Get("W2"))

	return font, nil
}

// pdfCIDFontType2 represents a CIDFont Type2 font dictionary.
type pdfCIDFontType2 struct {
	container *PdfIndirectObject
	skeleton  *fontSkeleton // Elements common to all font types

	encoder   textencoding.TextEncoder // !@#$ In skeleton?
	ttfParser *fonts.TtfType

	CIDSystemInfo PdfObject
	DW            PdfObject
	W             PdfObject
	DW2           PdfObject
	W2            PdfObject
	CIDToGIDMap   PdfObject

	// Mapping between unicode runes to widths.
	runeToWidthMap map[uint16]int

	// Also mapping between GIDs (glyph index) and width.
	gidToWidthMap map[uint16]int
}

// Encoder returns the font's text encoder.
func (font pdfCIDFontType2) Encoder() textencoding.TextEncoder {
	return font.encoder
}

// SetEncoder sets the encoder for the truetype font.
func (font pdfCIDFontType2) SetEncoder(encoder textencoding.TextEncoder) {
	font.encoder = encoder
}

// GetGlyphCharMetrics returns the character metrics for the specified glyph.  A bool flag is
// returned to indicate whether or not the entry was found in the glyph to charcode mapping.
func (font pdfCIDFontType2) GetGlyphCharMetrics(glyph string) (fonts.CharMetrics, bool) {
	metrics := fonts.CharMetrics{}

	enc := textencoding.NewTrueTypeFontEncoder(font.ttfParser.Chars)

	// Convert the glyph to character code.
	rune, found := enc.GlyphToRune(glyph)
	if !found {
		common.Log.Debug("Unable to convert glyph %q to charcode (identity)", glyph)
		return metrics, false
	}

	w, found := font.runeToWidthMap[uint16(rune)]
	if !found {
		return metrics, false
	}
	metrics.GlyphName = glyph
	metrics.Wx = float64(w)

	return metrics, true
}

// ToPdfObject converts the pdfCIDFontType2 to a PDF representation.
func (font *pdfCIDFontType2) ToPdfObject() PdfObject {
	if font.container == nil {
		font.container = &PdfIndirectObject{}
	}
	d := font.skeleton.toDict("CIDFontType2")
	font.container.PdfObject = d

	if font.CIDSystemInfo != nil {
		d.Set("CIDSystemInfo", font.CIDSystemInfo)
	}
	if font.DW != nil {
		d.Set("DW", font.DW)
	}
	if font.DW2 != nil {
		d.Set("DW2", font.DW2)
	}
	if font.W != nil {
		d.Set("W", font.W)
	}
	if font.W2 != nil {
		d.Set("W2", font.W2)
	}
	if font.CIDToGIDMap != nil {
		d.Set("CIDToGIDMap", font.CIDToGIDMap)
	}

	return font.container
}

// newPdfCIDFontType2FromPdfObject creates a pdfCIDFontType2 object from a dictionary (either direct
// or via indirect object). If a problem occurs with loading an error is returned.
func newPdfCIDFontType2FromPdfObject(obj PdfObject, skeleton *fontSkeleton) (*pdfCIDFontType2, error) {
	if skeleton.subtype != "CIDFontType2" {
		common.Log.Debug("ERROR: Font SubType != CIDFontType2. font=%s", skeleton)
		return nil, ErrRangeError
	}

	font := &pdfCIDFontType2{skeleton: skeleton}
	d := skeleton.dict

	// CIDSystemInfo.
	obj = d.Get("CIDSystemInfo")
	if obj == nil {
		common.Log.Debug("ERROR: CIDSystemInfo (Required) missing. font=%s", skeleton)
		return nil, ErrRequiredAttributeMissing
	}
	font.CIDSystemInfo = obj

	// Optional attributes.
	font.DW = d.Get("DW")
	font.W = d.Get("W")
	font.DW2 = d.Get("DW2")
	font.W2 = d.Get("W2")
	font.CIDToGIDMap = d.Get("CIDToGIDMap")

	return font, nil
}

// NewCompositePdfFontFromTTFFile loads a composite font from a TTF font file. Composite fonts can
// be used to represent unicode fonts which can have multi-byte character codes, representing a wide
// range of values.
// It is represented by a Type0 Font with an underlying CIDFontType2 and an Identity-H encoding map.
// TODO: May be extended in the future to support a larger variety of CMaps and vertical fonts.
func NewCompositePdfFontFromTTFFile(filePath string) (*PdfFont, error) {
	// Load the truetype font data.
	ttf, err := fonts.TtfParse(filePath)
	if err != nil {
		common.Log.Debug("ERROR: while loading ttf font: %v", err)
		return nil, err
	}

	// Prepare the inner descendant font (CIDFontType2).
	skeleton := fontSkeleton{subtype: "Type0"}
	cidfont := &pdfCIDFontType2{skeleton: &skeleton}
	cidfont.ttfParser = &ttf

	// 2-byte character codes -> runes
	runes := []uint16{}
	for r := range ttf.Chars {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool {
		return runes[i] < runes[j]
	})

	skeleton.basefont = ttf.PostScriptName

	k := 1000.0 / float64(ttf.UnitsPerEm)

	if len(ttf.Widths) <= 0 {
		return nil, errors.New("ERROR: Missing required attribute (Widths)")
	}

	missingWidth := k * float64(ttf.Widths[0])

	// Construct a rune -> width map.
	runeToWidthMap := map[uint16]int{}
	gidToWidthMap := map[uint16]int{}
	for _, r := range runes {
		glyphIndex := ttf.Chars[r]

		w := k * float64(ttf.Widths[glyphIndex])
		runeToWidthMap[r] = int(w)
		gidToWidthMap[glyphIndex] = int(w)
	}
	cidfont.runeToWidthMap = runeToWidthMap
	cidfont.gidToWidthMap = gidToWidthMap

	// Default width.
	cidfont.DW = MakeInteger(int64(missingWidth))

	// Construct W array.  Stores character code to width mappings.
	wArr := &PdfObjectArray{}
	i := uint16(0)
	for int(i) < len(runes) {

		j := i + 1
		for int(j) < len(runes) {
			if runeToWidthMap[runes[i]] != runeToWidthMap[runes[j]] {
				break
			}
			j++
		}

		// The W maps from CID to width, here CID = GID.
		gid1 := ttf.Chars[runes[i]]
		gid2 := ttf.Chars[runes[j-1]]

		wArr.Append(MakeInteger(int64(gid1)))
		wArr.Append(MakeInteger(int64(gid2)))
		wArr.Append(MakeInteger(int64(runeToWidthMap[runes[i]])))

		i = j
	}
	cidfont.W = MakeIndirectObject(wArr)

	// Use identity character id (CID) to glyph id (GID) mapping.
	cidfont.CIDToGIDMap = MakeName("Identity")

	d := MakeDict()
	d.Set("Ordering", MakeString("Identity"))
	d.Set("Registry", MakeString("Adobe"))
	d.Set("Supplement", MakeInteger(0))
	cidfont.CIDSystemInfo = d

	// Make the font descriptor.
	descriptor := &PdfFontDescriptor{}
	descriptor.Ascent = MakeFloat(k * float64(ttf.TypoAscender))
	descriptor.Descent = MakeFloat(k * float64(ttf.TypoDescender))
	descriptor.CapHeight = MakeFloat(k * float64(ttf.CapHeight))
	descriptor.FontBBox = MakeArrayFromFloats([]float64{k * float64(ttf.Xmin), k * float64(ttf.Ymin), k * float64(ttf.Xmax), k * float64(ttf.Ymax)})
	descriptor.ItalicAngle = MakeFloat(float64(ttf.ItalicAngle))
	descriptor.MissingWidth = MakeFloat(k * float64(ttf.Widths[0]))

	// Embed the TrueType font program.
	ttfBytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		common.Log.Debug("ERROR: :Unable to read file contents: %v", err)
		return nil, err
	}

	stream, err := MakeStream(ttfBytes, NewFlateEncoder())
	if err != nil {
		common.Log.Debug("ERROR: Unable to make stream: %v", err)
		return nil, err
	}
	stream.PdfObjectDictionary.Set("Length1", MakeInteger(int64(len(ttfBytes))))
	descriptor.FontFile2 = stream

	if ttf.Bold {
		descriptor.StemV = MakeInteger(120)
	} else {
		descriptor.StemV = MakeInteger(70)
	}

	// Flags.
	//flags := 1 << 5 // Non-Symbolic.
	flags := uint32(0)
	if ttf.IsFixedPitch {
		flags |= 1
	}
	if ttf.ItalicAngle != 0 {
		flags |= 1 << 6
	}
	flags |= 1 << 2 // Symbolic.
	descriptor.Flags = MakeInteger(int64(flags))

	skeleton.fontDescriptor = descriptor

	// Make root Type0 font.
	type0 := pdfFontType0{
		skeleton: &skeleton,
		DescendantFont: &PdfFont{context: cidfont,
			fontSkeleton: fontSkeleton{subtype: "CIDFontType2"},
		},
		Encoding: MakeName("Identity-H"),
		encoder:  textencoding.NewTrueTypeFontEncoder(ttf.Chars),
	}

	// Build Font.
	font := PdfFont{
		fontSkeleton: skeleton,
		context:      &type0,
	}

	return &font, nil
}