package exif

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// testJPEG returns sample1.jpg, and the offset and end of its EXIF APP1
// segment, its first.
func testJPEG(t *testing.T) (data []byte, start, end int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(*dataDir, "sample1.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	start = bytes.Index(data, []byte("\xff\xe1"))
	if start < 0 || !bytes.HasPrefix(data[start+4:], []byte(exifHeader)) {
		t.Fatal("sample's first APP1 segment isn't EXIF")
	}
	end = start + 2 + int(binary.BigEndian.Uint16(data[start+2:]))
	return data, start, end
}

// segment returns a JPEG segment with marker code c and contents data.
func segment(c byte, data string) []byte {
	return append([]byte{0xff, c, byte((len(data) + 2) >> 8), byte(len(data) + 2)}, data...)
}

// insert returns data with ins inserted at off.
func insert(data []byte, off int, ins ...[]byte) []byte {
	out := bytes.Clone(data[:off])
	for _, b := range ins {
		out = append(out, b...)
	}
	return append(out, data[off:]...)
}

// xmpSegment is an APP1 segment with XMP, rather than EXIF.
var xmpSegment = segment(jpeg_APP1, "http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta/>")

func TestDecodeJPEGSegments(t *testing.T) {
	data, start, end := testJPEG(t)
	want, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	wantModel, err := want.Get(Model)
	if err != nil {
		t.Fatal(err)
	}

	// Without an EXIF segment before the image data, Decode decodes the
	// APP1 segment at the first FF E1, if it's EXIF.
	afterEOI := append(bytes.Clone(data[:start]), data[end:]...)
	// Make the sample's later APP1 segment, with XMP, an APP2 one.
	if xmp := bytes.Index(afterEOI, []byte("\xff\xe1")); xmp >= 0 {
		afterEOI[xmp+1] = 0xe2
	}
	afterEOI = append(afterEOI, data[start:end]...)
	for _, tt := range []struct {
		name string
		data []byte
	}{
		// As Photoshop writes it: XMP, then a Photoshop APP13, then EXIF.
		{"afterXMP", insert(data, start, xmpSegment, segment(0xed, "Photoshop 3.0\x00"))},
		// FF E1 within an earlier segment isn't a marker.
		{"afterFalseMarker", insert(data, start, segment(0xe2, "\xff\xe1\x00\x08notEXF"))},
		{"fillBytes", insert(data, start, xmpSegment, []byte{0xff, 0xff})},
		{"afterEOI", afterEOI},
		{"rawExif", data[start+4 : end]},
		{"tiff", data[start+4+len(exifHeader) : end]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			x, err := Decode(bytes.NewReader(tt.data))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !bytes.Equal(x.Raw, want.Raw) {
				t.Errorf("decoded %d bytes; want the %d of the EXIF segment", len(x.Raw), len(want.Raw))
			}
			if model, err := x.Get(Model); err != nil || model.String() != wantModel.String() {
				t.Errorf("Model = %v, %v; want %v", model, err, wantModel)
			}
		})
	}
}

func TestDecodeJPEGTruncated(t *testing.T) {
	data, start, end := testJPEG(t)
	withXMP := insert(data, start, xmpSegment)
	for _, n := range []int{start + 1, start + len(xmpSegment) - 1, start + len(xmpSegment) + 3, end + len(xmpSegment) - 1} {
		_, err := Decode(bytes.NewReader(withXMP[:n]))
		if !errors.Is(err, io.ErrUnexpectedEOF) || !IsCriticalError(err) {
			t.Errorf("Decode of first %d bytes: %v; want critical io.ErrUnexpectedEOF", n, err)
		}
	}
	// A truncated segment with EXIF is complete enough for the search for
	// FF E1 to decode it, which is the same segment.
	if _, err := Decode(bytes.NewReader(data[:end])); err != nil {
		t.Errorf("Decode of the EXIF of a JPEG without its image: %v", err)
	}
}

func TestFindJPEGExif(t *testing.T) {
	data, start, end := testJPEG(t)
	for _, tt := range []struct {
		name    string
		data    []byte
		off     int64
		wantErr error
	}{
		{"first", data, int64(start), nil},
		{"afterXMP", insert(data, start, xmpSegment), int64(start + len(xmpSegment)), nil},
		{"fillBytes", insert(data, start, []byte{0xff, 0xff, 0xff}), int64(start + 3), nil},
		{"restart", insert(data, start, []byte{0xff, jpegRST0}), int64(start + 2), nil},
		{"none", append(insert(data[:start], start, xmpSegment), data[end:]...), 0, ErrNoJPEGExif},
		{"notJPEG", data[start+4 : end], 0, ErrNoJPEGExif},
		{"empty", nil, 0, ErrNoJPEGExif},
		{"truncated", data[:start+3], 0, io.ErrUnexpectedEOF},
		{"truncatedData", data[:end-1], 0, io.ErrUnexpectedEOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			off, seg, err := FindJPEGExif(bytes.NewReader(tt.data))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("FindJPEGExif: %v; want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if off != tt.off {
				t.Errorf("offset = %d; want %d", off, tt.off)
			}
			if want := data[start+4 : end]; !bytes.Equal(seg, want) {
				t.Errorf("segment of %d bytes; want the %d of the EXIF segment", len(seg), len(want))
			}
		})
	}
}
