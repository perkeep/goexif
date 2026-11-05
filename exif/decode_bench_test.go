package exif

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func benchmarkDecode(b *testing.B, files [][]byte) {
	var n int64
	for _, f := range files {
		n += int64(len(f))
	}
	b.SetBytes(n)
	b.ReportAllocs()
	for b.Loop() {
		for _, f := range files {
			Decode(bytes.NewReader(f))
		}
	}
}

func BenchmarkDecodeSample1(b *testing.B) {
	data, err := os.ReadFile(filepath.Join(*dataDir, "sample1.jpg"))
	if err != nil {
		b.Fatal(err)
	}
	benchmarkDecode(b, [][]byte{data})
}

func BenchmarkDecodeSamples(b *testing.B) {
	dir := filepath.Join(*dataDir, "samples")
	ents, err := os.ReadDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	var files [][]byte
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".jpg") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			b.Fatal(err)
		}
		files = append(files, data)
	}
	benchmarkDecode(b, files)
}
