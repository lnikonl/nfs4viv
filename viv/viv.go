// Package viv reads and rewrites the BIGF archives used by the Need for Speed
// 3/4 game data. Every car in Need for Speed 4 stores its resources (fedata
// text, geometry, textures, audio) inside its own car.viv file.
package viv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// Magic is the four byte signature every VIV archive starts with.
var Magic = []byte("BIGF")

const (
	headerSize = 16
	maxEntries = 1 << 20
)

// Entry is a single named blob inside a VIV archive.
type Entry struct {
	Name string
	Data []byte
}

// File is a parsed VIV archive. Directory order is preserved so a rewrite
// keeps the layout as close to the original as possible.
type File struct {
	Entries []Entry
}

// Read parses a VIV archive from raw file contents.
func Read(data []byte) (*File, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("viv: file of %d bytes is too small for a header", len(data))
	}
	if !bytes.Equal(data[:4], Magic) {
		return nil, fmt.Errorf("viv: bad magic %q, expected BIGF", data[:4])
	}
	entries := int(int32(binary.BigEndian.Uint32(data[8:12])))
	if entries < 0 || entries > maxEntries {
		return nil, fmt.Errorf("viv: implausible entry count %d", entries)
	}
	file := &File{}
	position := headerSize
	for index := 0; index < entries; index++ {
		if position+8 > len(data) {
			return nil, fmt.Errorf("viv: truncated directory entry %d", index)
		}
		offset := int(int32(binary.BigEndian.Uint32(data[position : position+4])))
		length := int(int32(binary.BigEndian.Uint32(data[position+4 : position+8])))
		position += 8
		end := bytes.IndexByte(data[position:], 0)
		if end < 0 {
			return nil, fmt.Errorf("viv: unterminated name in directory entry %d", index)
		}
		name := string(data[position : position+end])
		position += end + 1
		if offset < 0 || length < 0 || offset > len(data) || length > len(data)-offset {
			return nil, fmt.Errorf("viv: entry %q points outside the file (offset=0x%X length=0x%X)", name, offset, length)
		}
		file.Entries = append(file.Entries, Entry{Name: name, Data: data[offset : offset+length]})
	}
	return file, nil
}

// ReadFile parses the VIV archive stored at path.
func ReadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := Read(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

// Find returns the index of the first entry with the given name, ignoring
// case.
func (f *File) Find(name string) (int, bool) {
	for index := range f.Entries {
		if strings.EqualFold(f.Entries[index].Name, name) {
			return index, true
		}
	}
	return 0, false
}

// Get returns the data of the named entry.
func (f *File) Get(name string) ([]byte, bool) {
	if index, ok := f.Find(name); ok {
		return f.Entries[index].Data, true
	}
	return nil, false
}

// Set replaces the data of an existing entry, preserving its position.
func (f *File) Set(name string, data []byte) bool {
	if index, ok := f.Find(name); ok {
		f.Entries[index].Data = data
		return true
	}
	return false
}

// Add appends a new entry. It reports false if the name already exists.
func (f *File) Add(name string, data []byte) bool {
	if _, ok := f.Find(name); ok {
		return false
	}
	f.Entries = append(f.Entries, Entry{Name: name, Data: data})
	return true
}

// Remove deletes the named entry.
func (f *File) Remove(name string) bool {
	if index, ok := f.Find(name); ok {
		f.Entries = append(f.Entries[:index], f.Entries[index+1:]...)
		return true
	}
	return false
}

// Bytes serializes the archive. Data pool offsets and the total length are
// recalculated for the current entry set.
func (f *File) Bytes() ([]byte, error) {
	directorySize := headerSize
	for _, entry := range f.Entries {
		if entry.Name == "" {
			return nil, fmt.Errorf("viv: entry with an empty name")
		}
		if strings.IndexByte(entry.Name, 0) >= 0 {
			return nil, fmt.Errorf("viv: entry name %q contains a NUL byte", entry.Name)
		}
		directorySize += len(entry.Name) + 9
	}
	poolOffset := directorySize
	totalSize := poolOffset
	for _, entry := range f.Entries {
		totalSize += len(entry.Data)
	}
	out := make([]byte, totalSize)
	copy(out, Magic)
	binary.BigEndian.PutUint32(out[4:8], uint32(totalSize))
	binary.BigEndian.PutUint32(out[8:12], uint32(len(f.Entries)))
	binary.BigEndian.PutUint32(out[12:16], uint32(poolOffset))
	position := headerSize
	dataPosition := poolOffset
	for _, entry := range f.Entries {
		binary.BigEndian.PutUint32(out[position:position+4], uint32(dataPosition))
		binary.BigEndian.PutUint32(out[position+4:position+8], uint32(len(entry.Data)))
		position += 8
		position += copy(out[position:], entry.Name)
		out[position] = 0
		position++
		copy(out[dataPosition:], entry.Data)
		dataPosition += len(entry.Data)
	}
	return out, nil
}
