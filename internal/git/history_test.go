package git

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func buildTar(t *testing.T, entries []struct {
	name     string
	typeflag byte
	linkname string
	body     string
}) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Linkname: e.linkname,
			Mode:     0o644,
			Size:     int64(len(e.body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return &buf
}

func TestExtractTarNormalEntries(t *testing.T) {
	dest := t.TempDir()
	buf := buildTar(t, []struct {
		name     string
		typeflag byte
		linkname string
		body     string
	}{
		{name: "dir/", typeflag: tar.TypeDir},
		{name: "dir/file.txt", typeflag: tar.TypeReg, body: "hello"},
	})

	if err := extractTar(buf, dest); err != nil {
		t.Fatalf("extractTar: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected content: %q", data)
	}
}

func TestExtractTarRejectsPathTraversal(t *testing.T) {
	dest := t.TempDir()
	buf := buildTar(t, []struct {
		name     string
		typeflag byte
		linkname string
		body     string
	}{
		{name: "../escape.txt", typeflag: tar.TypeReg, body: "evil"},
	})

	if err := extractTar(buf, dest); err == nil {
		t.Fatal("expected error for path-traversal entry, got nil")
	}
}

func TestExtractTarRejectsAbsoluteName(t *testing.T) {
	dest := t.TempDir()
	abs := "/etc/evil.txt"
	if runtime.GOOS == "windows" {
		abs = `C:\evil.txt`
	}
	buf := buildTar(t, []struct {
		name     string
		typeflag byte
		linkname string
		body     string
	}{
		{name: abs, typeflag: tar.TypeReg, body: "evil"},
	})

	if err := extractTar(buf, dest); err == nil {
		t.Fatal("expected error for absolute entry name, got nil")
	}
}

func TestExtractTarRejectsSymlinkEscape(t *testing.T) {
	dest := t.TempDir()
	buf := buildTar(t, []struct {
		name     string
		typeflag byte
		linkname string
		body     string
	}{
		{name: "link", typeflag: tar.TypeSymlink, linkname: "../../outside"},
	})

	if err := extractTar(buf, dest); err == nil {
		t.Fatal("expected error for symlink escaping destDir, got nil")
	}
}

func TestExtractTarRejectsAbsoluteSymlinkTarget(t *testing.T) {
	dest := t.TempDir()
	abs := "/etc/passwd"
	if runtime.GOOS == "windows" {
		abs = `C:\Windows\System32\config\SAM`
	}
	buf := buildTar(t, []struct {
		name     string
		typeflag byte
		linkname string
		body     string
	}{
		{name: "link", typeflag: tar.TypeSymlink, linkname: abs},
	})

	if err := extractTar(buf, dest); err == nil {
		t.Fatal("expected error for absolute symlink target, got nil")
	}
}

func TestExtractTarAllowsSafeSymlink(t *testing.T) {
	dest := t.TempDir()
	probeDir := t.TempDir()
	if err := os.Symlink("probe-target", filepath.Join(probeDir, "probe-link")); err != nil {
		t.Skipf("creating symlinks is not permitted on this host: %v", err)
	}
	buf := buildTar(t, []struct {
		name     string
		typeflag byte
		linkname string
		body     string
	}{
		{name: "target.txt", typeflag: tar.TypeReg, body: "hi"},
		{name: "link", typeflag: tar.TypeSymlink, linkname: "target.txt"},
	})

	if err := extractTar(buf, dest); err != nil {
		t.Fatalf("extractTar: %v", err)
	}

	resolved, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if resolved != "target.txt" {
		t.Fatalf("unexpected link target: %q", resolved)
	}
}
