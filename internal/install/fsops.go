package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func readText(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))), nil
}

func sameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil || sa.Size() != sb.Size() {
		return false
	}
	ba, err1 := os.ReadFile(a)
	bb, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(ba, bb)
}

func sha256File(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func listFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

func sameTree(a, b string) bool {
	if !isDir(b) {
		return false
	}
	fa, err1 := listFiles(a)
	fb, err2 := listFiles(b)
	if err1 != nil || err2 != nil || len(fa) != len(fb) {
		return false
	}
	for _, rel := range fa {
		o := filepath.Join(b, rel)
		if !isFile(o) || !sameFile(filepath.Join(a, rel), o) {
			return false
		}
	}
	return true
}

func copyFileRaw(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, st.ModTime(), st.ModTime())
}

func (in *Installer) ensureBackupDir() string {
	if in.backup == "" {
		base := filepath.Join(in.opts.BackupRoot, "bk-"+in.opts.Now().Format("0102-150405"))
		p := base
		for n := 2; exists(p); n++ {
			p = base + "-" + strconv.Itoa(n)
		}
		in.backup = p
	}
	return in.backup
}

func (in *Installer) backupFile(rel, root, prefix string) error {
	if in.opts.DryRun {
		return nil
	}
	dst := filepath.Join(in.ensureBackupDir(), prefix, rel)
	if exists(dst) {
		return nil
	}
	return copyFileRaw(filepath.Join(root, rel), dst)
}

func (in *Installer) backupTree(dir, root, prefix string) error {
	files, err := listFiles(dir)
	if err != nil {
		return err
	}
	relDir, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := in.backupFile(filepath.Join(relDir, f), root, prefix); err != nil {
			return err
		}
	}
	return nil
}

func (in *Installer) writeFile(p string, data []byte) error {
	if in.opts.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (in *Installer) copyFile(src, dst string) error {
	if in.opts.DryRun {
		return nil
	}
	return copyFileRaw(src, dst)
}

func (in *Installer) copyTree(src, dst string) error {
	if in.opts.DryRun {
		return nil
	}
	files, err := listFiles(src)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := copyFileRaw(filepath.Join(src, f), filepath.Join(dst, f)); err != nil {
			return err
		}
	}
	return nil
}

func (in *Installer) removeAll(p string) error {
	if in.opts.DryRun {
		return nil
	}
	return os.RemoveAll(p)
}

func (in *Installer) copyIfChanged(src, dstDir, name, prefix string) (bool, error) {
	dst := filepath.Join(dstDir, name)
	if isFile(dst) && sameFile(src, dst) {
		return false, nil
	}
	if isFile(dst) {
		if err := in.backupFile(name, dstDir, prefix); err != nil {
			return false, err
		}
	}
	return true, in.copyFile(src, dst)
}
