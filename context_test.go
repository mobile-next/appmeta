package appmeta

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnAlreadyCancelledContextIsReportedAsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := buildMinimalAPK(t)
	_, err := ParseContext(ctx, bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestABlockingReaderIsAbandonedWhenTheDeadlinePasses(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := ParseContext(ctx, blockingReader{unblock: unblock}, 1<<20)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("returned after %v", elapsed)
	}
}

func TestReadsStopOnceTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	data := buildAcmeShopAPK(t)
	reader := cancelAfterReads{r: bytes.NewReader(data), remaining: 3, cancel: cancel}
	_, err := ParseContext(ctx, &reader, int64(len(data)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

type blockingReader struct {
	unblock chan struct{}
}

func (b blockingReader) ReadAt([]byte, int64) (int, error) {
	<-b.unblock
	return 0, errors.New("unblocked")
}

type cancelAfterReads struct {
	r         *bytes.Reader
	remaining int
	cancel    context.CancelFunc
}

func (c *cancelAfterReads) ReadAt(p []byte, off int64) (int, error) {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.r.ReadAt(p, off)
}
