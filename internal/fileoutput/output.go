package fileoutput

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"fix-multiplexer/internal/config"
)

// Message contains the upstream snapshot, never a downstream reconstructed
// message. FIX is base64 in JSON to preserve even non-UTF-8 field bytes.
type Message struct {
	MsgType         string `json:"msg_type"`
	UpstreamSeqNum  int    `json:"upstream_seq_num"`
	PossDupFlag     string `json:"poss_dup_flag,omitempty"`
	OrigSendingTime string `json:"orig_sending_time,omitempty"`
	FIX             []byte `json:"fix_base64"`
}
type Record struct {
	ReceivedAt time.Time `json:"received_at"`
	Message
}
type Options struct {
	Now       func() time.Time
	OpenStore func(string) (Store, error)
	OnFailure func(name, reason string)
}
type Snapshot struct {
	Name             string
	Healthy          bool
	Written, Dropped uint64
	QueueBytes       int64
	QueueDepth       int
}
type queued struct {
	at   time.Time
	line []byte
}
type output struct {
	config           config.Output
	types            map[string]bool
	store            Store
	onFailure        func(string, string)
	mu               sync.Mutex
	failed, closed   bool
	written, dropped uint64
	used             int64
	queue            chan queued
	done             chan struct{}
	file             File
	size             int64
	day              string
}
type Set struct {
	outputs []*output
	now     func() time.Time
}

func New(configs []config.Output, allowed, denied []string, options Options) (*Set, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OpenStore == nil {
		options.OpenStore = OpenDirectory
	}
	set := &Set{now: options.Now}
	global := make(map[string]bool)
	for _, typ := range allowed {
		if !config.IsAdmin(typ) {
			global[typ] = true
		}
	}
	for _, typ := range denied {
		delete(global, typ)
	}
	for _, raw := range configs {
		c := raw.WithDefaults()
		if c.QueueMessages < 1 || c.QueueMessages > 4096 || c.QueueBytes < 4096 || c.QueueBytes > 64<<20 || c.MaxFileBytes < 4096 || c.MaxFileBytes > 1<<30 || c.MaxFiles < 1 || c.MaxFiles > 128 {
			_ = set.Close()
			return nil, errors.New("invalid file output limits")
		}
		store, err := options.OpenStore(c.Directory)
		if err != nil {
			_ = set.Close()
			return nil, errors.New("cannot open file output")
		}
		types := make(map[string]bool)
		list := c.ReceiveMessageTypes
		if list == nil {
			list = allowed
		}
		for _, typ := range list {
			if global[typ] {
				types[typ] = true
			}
		}
		o := &output{config: c, types: types, store: store, onFailure: options.OnFailure, queue: make(chan queued, c.QueueMessages), done: make(chan struct{})}
		// Check inventory and enforce configured closed-file retention before any
		// engine starts. No existing file is reopened for append after a crash.
		if err = o.trim(c.MaxFiles); err != nil {
			_ = store.Close()
			_ = set.Close()
			return nil, errors.New("cannot initialize file output retention")
		}
		set.outputs = append(set.outputs, o)
		go o.run()
	}
	return set, nil
}

func (s *Set) Publish(message Message) {
	if s == nil {
		return
	}
	var line []byte
	var at time.Time
	for _, o := range s.outputs {
		if !o.types[message.MsgType] {
			continue
		}
		if line == nil {
			// JSON owns a snapshot before returning to the engine callback.
			at = s.now().UTC()
			var err error
			line, err = json.Marshal(Record{ReceivedAt: at, Message: message})
			if err != nil {
				for _, target := range s.outputs {
					if target.types[message.MsgType] {
						target.mu.Lock()
						target.dropped++
						target.mu.Unlock()
						target.fail("encode_failed")
					}
				}
				return
			}
			line = append(line, '\n')
		}
		o.enqueue(queued{at: at, line: line})
	}
}
func (o *output) enqueue(q queued) {
	o.mu.Lock()
	if o.closed || o.failed {
		o.dropped++
		o.mu.Unlock()
		return
	}
	reason := "queue_full"
	if int64(len(q.line)) > o.config.MaxFileBytes {
		reason = "record_too_large"
	} else if int64(len(q.line)) <= o.config.QueueBytes-o.used {
		select {
		case o.queue <- q:
			o.used += int64(len(q.line))
			o.mu.Unlock()
			return
		default:
		}
	}
	o.dropped++
	o.failed = true
	o.mu.Unlock()
	if o.onFailure != nil {
		o.onFailure(o.config.Name, reason)
	}
}
func (o *output) fail(reason string) {
	o.mu.Lock()
	first := !o.failed
	o.failed = true
	o.mu.Unlock()
	if first && o.onFailure != nil {
		o.onFailure(o.config.Name, reason)
	}
}
func (o *output) run() {
	defer close(o.done)
	for q := range o.queue {
		o.mu.Lock()
		failed := o.failed
		o.mu.Unlock()
		ok := false
		if !failed {
			if err := o.write(q); err != nil {
				o.fail("write_failed")
			} else {
				ok = true
			}
		}
		o.mu.Lock()
		o.used -= int64(len(q.line))
		if ok {
			o.written++
		} else {
			o.dropped++
		}
		o.mu.Unlock()
	}
	if o.file != nil && o.file.Close() != nil {
		o.fail("close_failed")
	}
	if o.store.Close() != nil {
		o.fail("lease_release_failed")
	}
}
func (o *output) write(q queued) error {
	day := q.at.UTC().Format("20060102")
	if o.file == nil || o.day != day || int64(len(q.line)) > o.config.MaxFileBytes-o.size {
		if o.file != nil {
			err := o.file.Close()
			o.file = nil
			if err != nil {
				return err
			}
		}
		if err := o.trim(o.config.MaxFiles - 1); err != nil {
			return err
		}
		f, err := o.store.Create(q.at)
		if err != nil {
			return err
		}
		o.file, o.day, o.size = f, day, 0
	}
	n, err := o.file.Write(q.line)
	if err == nil && n != len(q.line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return err
	}
	o.size += int64(n)
	return o.file.Sync()
}
func (o *output) trim(keep int) error {
	files, err := o.store.List()
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Size < 0 || file.Size > o.config.MaxFileBytes {
			return errors.New("existing capture file exceeds size limit")
		}
	}
	for len(files) > keep {
		if err = o.store.Remove(files[0].Name); err != nil {
			return err
		}
		files = files[1:]
	}
	return nil
}
func (s *Set) Snapshots() []Snapshot {
	if s == nil {
		return nil
	}
	out := make([]Snapshot, 0, len(s.outputs))
	for _, o := range s.outputs {
		o.mu.Lock()
		out = append(out, Snapshot{Name: o.config.Name, Healthy: !o.failed, Written: o.written, Dropped: o.dropped, QueueBytes: o.used, QueueDepth: len(o.queue)})
		o.mu.Unlock()
	}
	return out
}
func (s *Set) Healthy() bool {
	for _, state := range s.Snapshots() {
		if !state.Healthy {
			return false
		}
	}
	return true
}
func (s *Set) Close() error {
	if s == nil {
		return nil
	}
	for _, o := range s.outputs {
		o.mu.Lock()
		if !o.closed {
			o.closed = true
			close(o.queue)
		}
		o.mu.Unlock()
	}
	for _, o := range s.outputs {
		<-o.done
	}
	if !s.Healthy() {
		return errors.New("file output stopped with capture failures")
	}
	return nil
}
