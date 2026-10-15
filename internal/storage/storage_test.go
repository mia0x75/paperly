package storage

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSniffExtension(t *testing.T) {
	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"pdf", []byte("%PDF-1.7 tail"), "pdf"},
		{"png", []byte("\x89PNG\r\n\x1a\n...."), "png"},
		{"jpg", append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 8)...), "jpg"},
		{"gif87", []byte("GIF87a...."), "gif"},
		{"gif89", []byte("GIF89a...."), "gif"},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), make([]byte, 4)...), "webp"},
		{"exe 伪装", append([]byte{'M', 'Z', 0x90, 0x00}, make([]byte, 8)...), ""},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, wrapped, err := SniffExtension(bytes.NewReader(tc.head))
			require.NoError(t, err)
			require.Equal(t, tc.want, ext)
			if ext != "" {
				// After the head is replayed, the full original content should
				// be readable.
				all, err := io.ReadAll(wrapped)
				require.NoError(t, err)
				require.Equal(t, tc.head, all)
			}
		})
	}
}
