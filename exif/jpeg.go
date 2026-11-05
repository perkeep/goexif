package exif

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ErrNoJPEGExif is returned by FindJPEGExif for data that isn't a JPEG, or
// a JPEG without an APP1 segment with EXIF before its image data.
var ErrNoJPEGExif = errors.New("exif: no APP1 segment with EXIF in JPEG headers")

// errJPEGTruncated is returned by Decode for a JPEG that ends within its
// headers, before an APP1 segment with EXIF, if the search for FF E1
// doesn't find EXIF either.
var errJPEGTruncated = fmt.Errorf("exif: JPEG ended before its EXIF: %w", io.ErrUnexpectedEOF)

// JPEG marker codes, which follow an FF byte.
const (
	jpegTEM  = 0x01
	jpegRST0 = 0xd0
	jpegRST7 = 0xd7
	jpegSOI  = 0xd8
	jpegEOI  = 0xd9
	jpegSOS  = 0xda
)

// exifHeader starts the data of an APP1 segment with EXIF, before its TIFF
// header.
const exifHeader = "Exif\x00\x00"

// jpegReaders are the buffers with which Decode finds the EXIF of a JPEG.
type jpegReaders struct {
	br   *bufio.Reader
	read bytes.Buffer // all that br reads, for rereading if it finds no EXIF
}

var jpegReadersPool = sync.Pool{
	New: func() any { return &jpegReaders{br: bufio.NewReader(nil)} },
}

// maxPooledRead is the largest read buffer that jpegReaders.release keeps
// for reuse. JPEG headers before the EXIF are usually much smaller.
const maxPooledRead = 256 << 10

// getJPEGReaders returns pooled jpegReaders reading r, which the caller
// must release when done with them.
func getJPEGReaders(r io.Reader) *jpegReaders {
	jr := jpegReadersPool.Get().(*jpegReaders)
	jr.br.Reset(io.TeeReader(r, &jr.read))
	return jr
}

func (jr *jpegReaders) release() {
	jr.br.Reset(nil)
	if jr.read.Cap() > maxPooledRead {
		jr.read = bytes.Buffer{}
	}
	jr.read.Reset()
	jpegReadersPool.Put(jr)
}

// FindJPEGExif returns the first APP1 segment with EXIF of r, a JPEG,
// which is the segment that Decode decodes: off, the offset of the FF
// byte just before its marker code, after any fill bytes, and data, its
// contents after its length, which start with "Exif\x00\x00". It returns
// ErrNoJPEGExif without one before the image data, and an error wrapping
// io.ErrUnexpectedEOF if r ends first.
//
// FindJPEGExif may read past the segment.
func FindJPEGExif(r io.Reader) (off int64, data []byte, err error) {
	return findJPEGExif(bufio.NewReader(r))
}

func findJPEGExif(br *bufio.Reader) (off int64, data []byte, err error) {
	if soi, _ := br.Peek(2); len(soi) < 2 || soi[0] != 0xff || soi[1] != jpegSOI {
		return 0, nil, ErrNoJPEGExif
	}
	defer func() {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			err = errJPEGTruncated
		}
	}()
	n, _ := br.Discard(2) // bytes read from br
	readByte := func() (byte, error) {
		c, err := br.ReadByte()
		if err == nil {
			n++
		}
		return c, err
	}
	for {
		c, err := readByte()
		if err != nil {
			return 0, nil, err
		}
		if c != 0xff {
			return 0, nil, ErrNoJPEGExif // not a marker; malformed
		}
		// Any number of FF fill bytes may precede a marker's code.
		for c == 0xff {
			if c, err = readByte(); err != nil {
				return 0, nil, err
			}
		}
		marker := int64(n - 2)
		switch {
		case c == jpegSOS || c == jpegEOI || c == 0:
			return 0, nil, ErrNoJPEGExif
		case c == jpegTEM || jpegRST0 <= c && c <= jpegRST7:
			continue // no length or contents
		}
		var lenBytes [2]byte
		if _, err := io.ReadFull(br, lenBytes[:]); err != nil {
			return 0, nil, err
		}
		n += 2
		size := int(binary.BigEndian.Uint16(lenBytes[:])) - 2
		if size < 0 {
			return 0, nil, ErrNoJPEGExif
		}
		if c == jpeg_APP1 && size >= len(exifHeader) {
			head, err := br.Peek(len(exifHeader))
			if err != nil {
				return 0, nil, err
			}
			if string(head) == exifHeader {
				data := make([]byte, size)
				if _, err := io.ReadFull(br, data); err != nil {
					return 0, nil, err
				}
				return marker, data, nil
			}
		}
		d, err := br.Discard(size)
		n += d
		if err != nil {
			return 0, nil, err
		}
	}
}
