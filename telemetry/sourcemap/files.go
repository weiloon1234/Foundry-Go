package sourcemap

import (
	"context"
	"io"
	"os"
)

// Pin each directory separately so ancestor replacement cannot redirect children.
func openDirectory(root *os.Root, name string) (*os.Root, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, invalid("private source map directory is unsafe")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	linked, linkErr := root.Lstat(name)
	if err != nil || linkErr != nil || !sameFile(before, after) || !sameFile(before, linked) {
		child.Close()
		return nil, changed()
	}
	return child, nil
}

func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func readPrivate(ctx context.Context, root *os.Root, name string, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > int64(limit) {
		return nil, invalid("private source map is not a bounded regular file")
	}
	file, err := openRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !sameFile(before, opened) {
		return nil, changed()
	}
	data, err := readBounded(ctx, file, limit)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, invalid("private source map size exceeded")
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	linked, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !sameFile(before, after) || !sameFile(before, linked) || int64(len(data)) != before.Size() {
		return nil, changed()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func readBounded(ctx context.Context, reader io.Reader, limit int) ([]byte, error) {
	data := make([]byte, 0, min(limit, 32<<10))
	buffer := make([]byte, min(limit+1, 32<<10))
	for len(data) <= limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := limit + 1 - len(data)
		n, err := reader.Read(buffer[:min(len(buffer), remaining)])
		data = append(data, buffer[:n]...)
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
	return data, nil
}

func directoryEntries(ctx context.Context, root *os.Root, limit int) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(limit + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > limit {
		return nil, inventoryExceeded()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
