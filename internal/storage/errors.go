package storage

import "errors"

// ErrUnsupportedFileType indicates the file header is not one of the
// whitelisted types (PDF/PNG/JPG).
var ErrUnsupportedFileType = errors.New("unsupported file type")
