// Package storage provides filesystem storage utilities: hashing, type
// sniffing, persistence and cleanup. Zero business logic; no dependency on
// domain / repository.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// MaxUploadBytes is the upload size cap: 50MB.
const MaxUploadBytes = 50 << 20

// TempTaskPrefix is the relative prefix for task staging directories (page
// reorder lands here first and is moved after success). The worker validates
// paths against this prefix when consuming detail (path-traversal guard), with
// the same convention as the exam directory.
const TempTaskPrefix = "assets/tmp/"

// NewTempToken returns the hex string of a 16-byte random value, used to name
// temp directories. Random rather than a guessable sequence: directories under
// assets/tmp coexist across tasks, and a name collision would delete each
// other's files.
func NewTempToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate temp token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// TempTaskRel returns the relative staging path "assets/tmp/{token}" (stored in
// tasks.detail as a relative path).
func TempTaskRel(token string) string {
	return TempTaskPrefix + token
}

// TempTaskDir returns the absolute staging path (does not create it).
func TempTaskDir(dataDir, token string) string {
	return filepath.Join(dataDir, "assets", "tmp", token)
}

func PageDir(dataDir string, examID int64) string {
	return filepath.Join(dataDir, "assets", "exams", strconv.FormatInt(examID, 10))
}

func PagePath(dataDir string, examID int64, index int, ext string) string {
	return filepath.Join(PageDir(dataDir, examID), fmt.Sprintf("page_%03d.%s", index, ext))
}

// magicExt determines the extension from the file header magic number (does not
// trust the upload file name or Content-Type). Returns "" for unsupported types.
//
// Uploaded images are always converted to WebP, but the input whitelist still
// includes gif/webp: a parent may upload a webp straight from the phone gallery,
// or a gif converted by another tool; all go through decode -> WebP re-encode.
func magicExt(head []byte) string {
	switch {
	case len(head) >= 4 && string(head[:4]) == "%PDF":
		return "pdf"
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return "png"
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "jpg"
	case len(head) >= 6 && (string(head[:6]) == "GIF87a" || string(head[:6]) == "GIF89a"):
		return "gif"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "webp"
	default:
		return ""
	}
}

// SniffExtension sniffs the real file type from the reader's head. Returns
// (ext, wrappedReader, err); wrappedReader is for subsequent full reads (the
// head bytes are replayed).
func SniffExtension(r io.Reader) (string, io.Reader, error) {
	// 12 bytes: WebP's "WEBP" marker sits at offset 8..12, so 8 bytes is
	// not enough to decide.
	head := make([]byte, 12)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", nil, fmt.Errorf("read head: %w", err)
	}
	ext := magicExt(head[:n])
	return ext, io.MultiReader(strings.NewReader(string(head[:n])), r), nil
}
