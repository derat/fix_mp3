package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
)

const (
	id3HeaderSize = 10
	scanBytes     = 2048
)

func readHeader(f *os.File) (headerSize int64, err error) {
	b := make([]byte, id3HeaderSize)
	_, err = f.ReadAt(b, 0)
	if err != nil {
		return 0, err
	}

	if b[0] != 'I' || b[1] != 'D' || b[2] != '3' {
		return 0, fmt.Errorf("File starts with %v instead of \"ID3\"", b[0:3])
	}
	majorVersion := b[3]
	minorVersion := b[4]
	if majorVersion != 3 && majorVersion != 4 {
		return 0, fmt.Errorf("Unsupported major version %d", majorVersion)
	}
	log.Printf("ID3 v2.%d.%d", majorVersion, minorVersion)

	if b[5] != 0x0 {
		return 0, fmt.Errorf("Unsupported flags 0x%x", b[4])
	}

	tagSize := 0
	for i := 6; i < 10; i++ {
		if b[i]&0x80 != 0 {
			return 0, fmt.Errorf("High bit(s) set in size %v", b[6:10])
		}
		tagSize = tagSize << 7
		tagSize += int(b[i] & 0x7f)
	}
	log.Printf("Tag size is 0x%x", tagSize)
	log.Printf("Header size is 0x%x", tagSize+id3HeaderSize)
	return int64(tagSize + id3HeaderSize), nil
}

func readFrame(f *os.File, offset int64) error {
	if _, err := f.Seek(offset, 0); err != nil {
		return err
	}
	var hdr uint32
	if err := binary.Read(f, binary.BigEndian, &hdr); err != nil {
		return err
	}

	getBits := func(startBit, numBits uint) uint32 {
		return (hdr << startBit) >> (32 - numBits)
	}
	if getBits(0, 11) != 0x7ff {
		return fmt.Errorf("Missing sync at 0x%x (got 0x%x)", offset, getBits(0, 11))
	}
	if getBits(11, 2) != 0x3 {
		return fmt.Errorf("Unsupported MPEG Audio version at 0x%x (got 0x%x)", getBits(11, 2))
	}
	if getBits(13, 2) != 0x1 {
		return fmt.Errorf("Unsupported layer at 0x%x (got 0x%x)", getBits(13, 2))
	}
	log.Printf("Read MP3 frame at 0x%x", offset)
	return nil
}

func scanForFrame(f *os.File, start, length int64) (int64, error) {
	b := make([]byte, length)
	_, err := f.ReadAt(b, start)
	if err != nil {
		return 0, err
	}

	var offset int64
	for offset = start; offset < start+length; offset++ {
		if b[offset-start] == 0xff {
			if err = readFrame(f, offset); err == nil {
				return offset, nil
			}
		}
	}
	return 0, fmt.Errorf("Didn't find frame")
}

func writeTagSize(f *os.File, size int64) error {
	b := make([]byte, 4)
	rem := size
	for i := 0; i < 4; i++ {
		b[3-i] = byte(rem & 0x7f)
		rem = rem >> 7
	}
	log.Printf("Writing tag size 0x%x as %v", size, b)
	w, err := os.OpenFile(f.Name(), os.O_WRONLY, os.FileMode(0644))
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = w.WriteAt(b, 6)
	return err
}

func main() {
	force := flag.Bool("force", false, "Actually update the file")
	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %v [flags] FILENAME\n", os.Args[0])
		os.Exit(1)
	}

	fn := flag.Args()[0]
	f, err := os.Open(fn)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	log.Printf("Reading %v", fn)
	headerSize, err := readHeader(f)
	if err != nil {
		log.Fatal("Failed to read header: ", err)
	}
	if err = readFrame(f, headerSize); err != nil {
		log.Print("Failed to read frame: ", err)
		log.Print("Scanning for first MP3 frame...")
		frameOffset, err := scanForFrame(f, headerSize, scanBytes)
		if err != nil {
			log.Fatalf("Didn't find MP3 frame in first %v bytes starting at 0x%x: %v", scanBytes, headerSize, err)
		}
		log.Printf("Found MP3 frame at 0x%x", frameOffset)
		if *force {
			if err = writeTagSize(f, frameOffset-id3HeaderSize); err != nil {
				log.Fatalf("Failed to write updated tag size: %v", err)
			}
		}
	}
}
