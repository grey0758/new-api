package channeltrace

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shirou/gopsutil/disk"
)

const Retention = 24 * time.Hour
const bodyLimit = 16 << 20
const memoryLimit = 128 << 20
const archiveLimit = 2 << 30
const segmentLimit = 16 << 20
const reserve = 2 << 30

type record struct {
	meta        map[string]any
	capture     *capture
	contentType string
}

var archiveName = regexp.MustCompile(`^trace-[0-9]{8}T[0-9]{2}-[A-Za-z0-9]+-[0-9]{6}\.jsonl\.gz$`)

type recorder struct {
	instance string
	dir      string
	queue    chan record
	memory   atomic.Int64
	dropped  atomic.Int64
	written  atomic.Int64
	ioErrors atomic.Int64
	mu       sync.Mutex
	file     string
	segment  int
	clock    func() time.Time
}

func newRecorder(dir string) (*recorder, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("trace directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid trace directory")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	id := make([]byte, 8)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	r := &recorder{dir: dir, queue: make(chan record, 64), clock: time.Now, instance: hex.EncodeToString(id)}
	if err = r.maintain(); err != nil {
		return nil, err
	}
	r.stats()
	go r.run()
	return r, nil
}
func (r *recorder) run() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case e := <-r.queue:
			func() {
				defer func() {
					if e.capture != nil {
						r.memory.Add(-int64(len(e.capture.data)))
					}
				}()
				if e.capture != nil {
					e.meta["body"] = decode(e.capture.data, e.contentType)
					e.meta["body_bytes_seen"] = e.capture.seen
					e.meta["body_bytes_captured"] = len(e.capture.data)
					e.meta["body_sha256_captured"] = digest(e.capture.data)
					e.meta["body_complete"] = e.capture.complete
					e.meta["body_truncated"] = e.capture.seen > int64(len(e.capture.data))
					e.meta["capture_limited"] = e.capture.limited
				}
				b, err := common.Marshal(e.meta)
				if err == nil {
					err = r.write(append(b, '\n'))
				}
				if err != nil {
					r.ioErrors.Add(1)
				} else {
					r.written.Add(1)
				}
			}()
		case <-ticker.C:
			if r.maintain() != nil {
				r.ioErrors.Add(1)
			}
			r.stats()
			if r.dropped.Load() > 0 || r.ioErrors.Load() > 0 {
				common.SysError("channel trace has capture loss; inspect private stats file")
			}
		}
	}
}
func (r *recorder) submit(e record) {
	e.meta["time"] = r.clock().UTC().Format(time.RFC3339Nano)
	select {
	case r.queue <- e:
	default:
		r.dropped.Add(1)
		if e.capture != nil {
			r.memory.Add(-int64(len(e.capture.data)))
		}
	}
}
func (r *recorder) write(b []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	usage, err := disk.Usage(r.dir)
	if err != nil {
		return err
	}
	if usage.Free < reserve {
		return fmt.Errorf("trace disk reserve")
	}
	if r.instance == "" {
		r.instance = "test"
	}
	prefix := "trace-" + r.clock().UTC().Format("20060102T15") + "-" + r.instance + "-"
	if r.file == "" || !strings.HasPrefix(filepath.Base(r.file), prefix) {
		if err := r.maintainLocked(); err != nil {
			return err
		}
		r.segment = 0
		r.file = filepath.Join(r.dir, prefix+fmt.Sprintf("%06d.jsonl.gz", r.segment))
	}
	if st, err := os.Stat(r.file); err == nil && st.Size() >= segmentLimit {
		if err := r.maintainLocked(); err != nil {
			return err
		}
		r.segment++
		r.file = filepath.Join(r.dir, prefix+fmt.Sprintf("%06d.jsonl.gz", r.segment))
	}
	f, err := os.OpenFile(r.file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	z := gzip.NewWriter(f)
	_, err = z.Write(b)
	closeErr := z.Close()
	fileErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fileErr
}

// maintain runs even while idle. Only our exact archive names can be removed.
func (r *recorder) maintain() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maintainLocked()
}
func (r *recorder) maintainLocked() error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}
	type archive struct {
		path  string
		size  int64
		start time.Time
	}
	var files []archive
	var total int64
	for _, e := range entries {
		n := e.Name()
		if !archiveName.MatchString(n) || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		start, err := time.Parse("20060102T15", n[6:17])
		if err != nil {
			continue
		}
		st, err := e.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(r.dir, n)
		if !start.Add(Retention).After(r.clock()) {
			if err = os.Remove(path); err != nil {
				return err
			}
			if path == r.file {
				r.file = ""
			}
			continue
		}
		files = append(files, archive{path, st.Size(), start})
		total += st.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	for _, f := range files {
		if total <= archiveLimit {
			break
		}
		if err = os.Remove(f.path); err != nil {
			return err
		}
		total -= f.size
		if f.path == r.file {
			r.file = ""
		}
	}
	return nil
}
func (r *recorder) stats() {
	b, err := common.Marshal(map[string]any{"time": r.clock().UTC().Format(time.RFC3339Nano), "instance": r.instance, "retention_seconds": int64(Retention / time.Second), "body_limit_bytes": bodyLimit, "archive_limit_bytes": archiveLimit, "memory_bytes": r.memory.Load(), "dropped_records": r.dropped.Load(), "io_errors": r.ioErrors.Load(), "written_records": r.written.Load(), "queue_depth": len(r.queue)})
	if err != nil {
		return
	}
	tmp := filepath.Join(r.dir, ".stats-"+r.instance+".tmp")
	target := filepath.Join(r.dir, "stats-"+r.instance+".json")
	if os.WriteFile(tmp, b, 0600) == nil {
		if os.Rename(tmp, target) != nil {
			r.ioErrors.Add(1)
		}
	} else {
		r.ioErrors.Add(1)
	}
}

type capture struct {
	mu       sync.Mutex
	r        *recorder
	data     []byte
	seen     int64
	complete bool
	limited  bool
}

func (r *recorder) capture() *capture { return &capture{r: r} }
func (b *capture) add(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen += int64(len(p))
	n := min(len(p), bodyLimit-len(b.data))
	if n == 0 {
		return
	}
	if b.r.memory.Add(int64(n)) > memoryLimit {
		b.r.memory.Add(-int64(n))
		b.limited = true
		return
	}
	b.data = append(b.data, p[:n]...)
}
func (b *capture) take(complete bool) *capture {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := &capture{data: b.data, seen: b.seen, complete: complete, limited: b.limited}
	b.data = nil
	b.seen = 0
	return out
}
