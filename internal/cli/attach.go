package cli

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wd/internal/core"
)

// attachedFiles are the --attach paths, each checked to be a file that can be
// read, so a bad one refuses the command before anything is sent or recorded.
func (c *Cli) attachedFiles() ([]string, error) {
	var files []string
	for _, p := range c.Args.Lists["attach"] {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, fail("cannot attach %s: %v", p, errors.Unwrap(err))
		}
		if st.IsDir() {
			return nil, fail("cannot attach %s: it is a directory; attach the files in it", p)
		}
		f, err := os.Open(abs)
		if err != nil {
			return nil, fail("cannot attach %s: %v", p, errors.Unwrap(err))
		}
		f.Close()
		files = append(files, abs)
	}
	return files, nil
}

// store copies files into $WD_HOME/attachments/<work>/<n>-<name>, n counting
// on from the copies already there, and returns what was stored. A copy that
// fails takes back the ones made before it.
func (c *Cli) store(work string, files []string) ([]core.Attachment, error) {
	atts := []core.Attachment{}
	if len(files) == 0 {
		return atts, nil
	}
	dir := filepath.Join(c.Home, "attachments", work)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	n, err := lastStored(dir)
	if err != nil {
		return nil, err
	}
	for _, src := range files {
		n++
		a, err := copyIn(src, filepath.Join(dir, strconv.Itoa(n)+"-"+sanitised(filepath.Base(src))))
		if err != nil {
			discard(atts)
			return nil, fail("cannot attach %s: %v", src, err)
		}
		atts = append(atts, a)
	}
	return atts, nil
}

// lastStored is the highest n among dir's stored copies, 0 when none.
func lastStored(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		head, _, _ := strings.Cut(e.Name(), "-")
		if n, err := strconv.Atoi(head); err == nil && n > last {
			last = n
		}
	}
	return last, nil
}

// sanitised keeps a file name's letters, digits, dots, dashes and
// underscores, turning anything else into an underscore.
func sanitised(name string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, name)
	if strings.Trim(out, "._") == "" {
		return "file"
	}
	return out
}

func copyIn(src, dst string) (core.Attachment, error) {
	in, err := os.Open(src)
	if err != nil {
		return core.Attachment{}, err
	}
	defer in.Close()
	head := make([]byte, 512)
	k, err := io.ReadFull(in, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return core.Attachment{}, err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return core.Attachment{}, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return core.Attachment{}, err
	}
	size, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return core.Attachment{}, err
	}
	return core.Attachment{Name: filepath.Base(src), Path: dst, Size: size, MediaType: mediaType(src, head[:k])}, nil
}

// mediaType is the type the file's extension names, else the one its first
// bytes show, without parameters.
func mediaType(name string, head []byte) string {
	t := mime.TypeByExtension(filepath.Ext(name))
	if t == "" {
		t = http.DetectContentType(head)
	}
	t, _, _ = strings.Cut(t, ";")
	return strings.TrimSpace(t)
}

// discard removes stored copies whose message was never sent.
func discard(atts []core.Attachment) {
	for _, a := range atts {
		os.Remove(a.Path)
	}
}

// attachmentsOf is every file attached to work itself, oldest first.
func (c *Cli) attachmentsOf(work string) ([]core.Attachment, error) {
	events, err := c.Ledger.Events(work, nil)
	if err != nil {
		return nil, err
	}
	atts := []core.Attachment{}
	for _, e := range events {
		if e.Work == work && e.Kind == core.EventAttachment && e.Attachment != nil {
			atts = append(atts, *e.Attachment)
		}
	}
	return atts, nil
}
