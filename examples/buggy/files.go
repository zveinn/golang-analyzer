package buggy

import (
	"io"
	"os"
)

// ReadHeaderLeaky opens a file and returns without ever closing it.
func ReadHeaderLeaky(path string) ([]byte, error) {
	f, err := os.Open(path) // never closed
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 64)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

// WriteLogLeaky creates a file and forgets it on the error path and the
// happy path alike.
func WriteLogLeaky(path string, line string) error {
	f, err := os.Create(path) // never closed
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	return err
}

// ReadHeaderOK closes via defer — must NOT be flagged.
func ReadHeaderOK(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 64)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

// OpenForCaller hands the file to its caller, which owns closing it —
// must NOT be flagged.
func OpenForCaller(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return f, nil
}
