// broadcast package
// code from dotcloud/docker
package main

import (
	"bytes"
	"io"
	"sync"
	"time"
)

type StreamWriter struct {
	wc     io.WriteCloser
	stream string
}

type WriteBroadcaster struct {
	sync.Mutex
	buf     *bytes.Buffer
	writers map[StreamWriter]bool
	closed  bool
}

func NewWriteBroadcaster() *WriteBroadcaster {
	bc := &WriteBroadcaster{
		writers: make(map[StreamWriter]bool),
		buf:     bytes.NewBuffer(nil),
		closed:  false,
	}
	return bc
}

func (w *WriteBroadcaster) AddWriter(writer io.WriteCloser, stream string) {
	w.Lock()
	defer w.Unlock()
	if w.closed {
		writer.Close()
		return
	}
	sw := StreamWriter{wc: writer, stream: stream}
	w.writers[sw] = true
}

func (wb *WriteBroadcaster) NewReader(name string) ([]byte, *io.PipeReader) {
	r, w := io.Pipe()
	wb.AddWriter(w, name)
	return wb.buf.Bytes(), r
}

func (w *WriteBroadcaster) Write(p []byte) (n int, err error) {
	w.Lock()
	defer w.Unlock()
	w.buf.Write(p)
	for sw := range w.writers {
		// timeout handler
		done := make(chan bool)
		go func() {
			ok := true
			if n, err := sw.wc.Write(p); err != nil || n != len(p) {
				// On error, evict the writer
				ok = false
			}
			done <- ok
		}()
		select {
		case ok := <-done:
			if !ok {
				delete(w.writers, sw)
			}
		case <-time.After(time.Second * 1):
			// timeout just delete writers
			Debugf("timeout: %s", sw.stream)
			delete(w.writers, sw)
		}
	}
	return len(p), nil
}

func (w *WriteBroadcaster) CloseWriters() error {
	w.Lock()
	defer w.Unlock()
	for sw := range w.writers {
		sw.wc.Close()
	}
	w.writers = make(map[StreamWriter]bool)
	w.closed = true
	return nil
}

// nop writer
type NopWriter struct{}

func (*NopWriter) Write(buf []byte) (int, error) {
	return len(buf), nil
}

type nopWriteCloser struct {
	io.Writer
}

func (w *nopWriteCloser) Close() error { return nil }

func NopWriteCloser(w io.Writer) io.WriteCloser {
	return &nopWriteCloser{w}
}
