package savebak

import (
	"archive/zip"
	"bufio"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type entry struct {
	rel  string
	dir  bool
	size int64
	mode fs.FileMode
	mod  time.Time
}

var storeExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".zip": true, ".gz": true, ".7z": true,
	".xz": true, ".bz2": true, ".zst": true, ".ogg": true, ".mp3": true, ".jar": true,
}

type reporter struct {
	fn   func(Progress)
	last time.Time
	p    Progress
}

func (r *reporter) report(force bool) {
	if r.fn == nil {
		return
	}
	now := time.Now()
	if !force && now.Sub(r.last) < 100*time.Millisecond {
		return
	}
	r.last = now
	r.fn(r.p)
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
	rep *reporter
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if c.rep != nil {
		c.rep.p.Bytes += int64(n)
		c.rep.report(false)
	}
	return n, err
}

func scan(ctx context.Context, root string) ([]entry, int64, int, error) {
	var out []entry
	var total int64
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !d.IsDir() && !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		e := entry{rel: rel, dir: d.IsDir(), mode: info.Mode(), mod: info.ModTime()}
		if !e.dir {
			e.size = info.Size()
			total += e.size
			files++
		}
		out = append(out, e)
		return nil
	})
	return out, total, files, err
}

func (m *Manager) Backup(ctx context.Context, mode, world, label string, progress func(Progress)) (Backup, error) {
	label = CleanLabel(label)
	if err := validSave(mode, world); err != nil {
		return Backup{}, err
	}
	if err := m.checkRoot(); err != nil {
		return Backup{}, err
	}
	if err := m.checkRunning(); err != nil {
		return Backup{}, err
	}
	src := m.SaveDir(mode, world)
	st, err := os.Stat(src)
	if err != nil {
		return Backup{}, fmt.Errorf("save folder: %w", err)
	}
	if !st.IsDir() {
		return Backup{}, fmt.Errorf("%s is not a folder", src)
	}
	rep := &reporter{fn: progress, p: Progress{Phase: "scan"}}
	rep.report(true)
	ents, total, files, err := scan(ctx, src)
	if err != nil {
		return Backup{}, err
	}
	dir := m.BackupDir(mode, world)
	if err := m.checkFree(dir, uint64(total)+uint64(len(ents))*200+32<<20); err != nil {
		return Backup{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Backup{}, err
	}
	t := m.now()
	final := m.freePath(dir, world, t, label)
	part := filepath.Join(dir, "."+filepath.Base(final)+".partial")
	rep.p = Progress{Phase: "backup", TotalFiles: files, TotalBytes: total}
	rep.report(true)
	err = writeZip(ctx, src, ents, part, rep)
	if err == nil {
		err = m.checkRunning()
	}
	if err == nil {
		err = os.Rename(part, final)
	}
	if err != nil {
		os.Remove(part)
		return Backup{}, err
	}
	rep.report(true)
	fi, _ := os.Stat(final)
	b := Backup{Path: final, Mode: mode, World: world, Label: label}
	b.Time, _, _ = parseName(world, filepath.Base(final))
	if fi != nil {
		b.Size = fi.Size()
	}
	return b, nil
}

func writeZip(ctx context.Context, src string, ents []entry, dst string, rep *reporter) (err error) {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	bw := bufio.NewWriterSize(f, 1<<20)
	zw := zip.NewWriter(bw)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestSpeed)
	})
	for _, e := range ents {
		if err := ctx.Err(); err != nil {
			return err
		}
		h := &zip.FileHeader{Name: e.rel, Modified: e.mod}
		h.SetMode(e.mode)
		if e.dir {
			h.Name += "/"
			h.Method = zip.Store
			if _, err := zw.CreateHeader(h); err != nil {
				return err
			}
			continue
		}
		h.Method = zip.Deflate
		if storeExt[strings.ToLower(path.Ext(e.rel))] || e.size == 0 {
			h.Method = zip.Store
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(filepath.Join(src, filepath.FromSlash(e.rel)))
		if err != nil {
			return err
		}
		n, err := io.Copy(w, &ctxReader{ctx: ctx, r: in, rep: rep})
		in.Close()
		if err != nil {
			return err
		}
		if n != e.size {
			return fmt.Errorf("%s changed size during the backup", e.rel)
		}
		rep.p.Files++
		rep.report(false)
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

func entryPath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("unsafe path in backup: %q", name)
	}
	c := path.Clean(name)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("unsafe path in backup: %q", name)
	}
	return filepath.FromSlash(c), nil
}

type plan struct {
	f    *zip.File
	rel  string
	dir  bool
	size int64
}

func validate(zfs []*zip.File) ([]plan, int64, int, error) {
	var out []plan
	var total int64
	files := 0
	seen := map[string]bool{}
	for _, f := range zfs {
		rel, err := entryPath(f.Name)
		if err != nil {
			return nil, 0, 0, err
		}
		mode := f.Mode()
		dir := strings.HasSuffix(f.Name, "/") || mode.IsDir()
		if !dir && !mode.IsRegular() {
			return nil, 0, 0, fmt.Errorf("backup entry %q is not a regular file", f.Name)
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return nil, 0, 0, fmt.Errorf("duplicate entry %q in backup", f.Name)
		}
		seen[key] = true
		p := plan{f: f, rel: rel, dir: dir}
		if !dir {
			p.size = int64(f.UncompressedSize64)
			if p.size < 0 {
				return nil, 0, 0, fmt.Errorf("entry %q too large", f.Name)
			}
			total += p.size
			files++
		}
		out = append(out, p)
	}
	return out, total, files, nil
}

type RestoreResult struct {
	Safety  *Backup
	Warning string
}

func (m *Manager) Restore(ctx context.Context, b Backup, progress func(Progress)) (RestoreResult, error) {
	var res RestoreResult
	if err := m.owns(b); err != nil {
		return res, err
	}
	if err := m.checkRunning(); err != nil {
		return res, err
	}
	zr, err := zip.OpenReader(b.Path)
	if err != nil {
		return res, fmt.Errorf("open backup: %w", err)
	}
	defer zr.Close()
	plans, total, files, err := validate(zr.File)
	if err != nil {
		return res, err
	}
	dst := m.SaveDir(b.Mode, b.World)
	parent := filepath.Dir(dst)
	exists := false
	if st, err := os.Stat(dst); err == nil {
		if !st.IsDir() {
			return res, fmt.Errorf("%s is not a folder", dst)
		}
		exists = true
	}
	if err := m.checkFree(parent, uint64(total)+uint64(len(plans))*4096+32<<20); err != nil {
		return res, err
	}
	if exists {
		sb, err := m.Backup(ctx, b.Mode, b.World, SafetyLabel, func(p Progress) {
			if progress != nil {
				p.Phase = "safety"
				progress(p)
			}
		})
		if err != nil {
			return res, fmt.Errorf("safety backup of the current save failed, nothing was restored: %w", err)
		}
		res.Safety = &sb
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return res, err
	}
	tmp, err := os.MkdirTemp(parent, "."+b.World+".restore-")
	if err != nil {
		return res, err
	}
	swapped := false
	defer func() {
		if !swapped {
			os.RemoveAll(tmp)
		}
	}()
	rep := &reporter{fn: progress, p: Progress{Phase: "restore", TotalFiles: files, TotalBytes: total}}
	rep.report(true)
	if err := extract(ctx, plans, tmp, rep); err != nil {
		return res, fmt.Errorf("restore failed, the save was not changed: %w", err)
	}
	rep.p.Phase = "verify"
	rep.report(true)
	if err := verifyTree(tmp, plans); err != nil {
		return res, fmt.Errorf("restore failed verification, the save was not changed: %w", err)
	}
	if err := m.checkRunning(); err != nil {
		return res, err
	}
	old := ""
	if exists {
		old = dst + ".old-" + m.now().Format(stampLayout)
		if err := os.Rename(dst, old); err != nil {
			return res, fmt.Errorf("could not move the current save aside, nothing was changed: %w", err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		if old != "" {
			if rerr := os.Rename(old, dst); rerr != nil {
				return res, fmt.Errorf("restore failed (%v) and putting the old save back failed too; it is at %s: %w", err, old, rerr)
			}
		}
		return res, fmt.Errorf("restore failed, the save was not changed: %w", err)
	}
	swapped = true
	if old != "" {
		if err := os.RemoveAll(old); err != nil {
			res.Warning = fmt.Sprintf("restored, but the old save copy could not be removed: %s (%v)", old, err)
		}
	}
	rep.report(true)
	return res, nil
}

func extract(ctx context.Context, plans []plan, root string, rep *reporter) error {
	for _, p := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(root, p.rel)
		if p.dir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractFile(ctx, p, target, rep); err != nil {
			return fmt.Errorf("%s: %w", p.f.Name, err)
		}
		rep.p.Files++
		rep.report(false)
	}
	for i := len(plans) - 1; i >= 0; i-- {
		if plans[i].dir && !plans[i].f.Modified.IsZero() {
			os.Chtimes(filepath.Join(root, plans[i].rel), plans[i].f.Modified, plans[i].f.Modified)
		}
	}
	return nil
}

func extractFile(ctx context.Context, p plan, target string, rep *reporter) (err error) {
	rc, err := p.f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	perm := p.f.Mode().Perm() | 0o600
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, &ctxReader{ctx: ctx, r: rc, rep: rep})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != p.size {
		return fmt.Errorf("size mismatch: got %d bytes, expected %d", n, p.size)
	}
	if !p.f.Modified.IsZero() {
		os.Chtimes(target, p.f.Modified, p.f.Modified)
	}
	return nil
}

func verifyTree(root string, plans []plan) error {
	want := map[string]plan{}
	for _, p := range plans {
		want[filepath.ToSlash(p.rel)] = p
	}
	got := 0
	err := filepath.WalkDir(root, func(pth string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if pth == root {
			return nil
		}
		rel, _ := filepath.Rel(root, pth)
		rel = filepath.ToSlash(rel)
		p, ok := want[rel]
		if !ok {
			if d.IsDir() {
				return nil
			}
			return fmt.Errorf("unexpected file %s", rel)
		}
		if p.dir != d.IsDir() {
			return fmt.Errorf("%s has the wrong type", rel)
		}
		if !p.dir {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() != p.size {
				return fmt.Errorf("%s has size %d, expected %d", rel, info.Size(), p.size)
			}
		}
		got++
		return nil
	})
	if err != nil {
		return err
	}
	if got != len(plans) {
		return fmt.Errorf("restored %d entries, the backup has %d", got, len(plans))
	}
	return nil
}
