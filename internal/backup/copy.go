package backup

import (
	"io"
	"os"
)

// Streaming rather than os.ReadFile: exam asset directories can hold whole
// volumes of images, and reading them all into memory is wasteful on a Box
// (small single-machine) host.
func copyFileBytes(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if err != nil {
		_ = out.Close()
		return n, err
	}
	return n, out.Close()
}
