package mail

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeSender struct {
	failures int
	calls    int
}

func (f *fakeSender) Send(_ context.Context, _ Message) error {
	f.calls++
	if f.calls <= f.failures {
		return errors.New("smtp down")
	}
	return nil
}

func TestQueueRetries(t *testing.T) {
	tests := []struct {
		name      string
		failures  int
		wantCalls int
	}{
		{"succeeds first try", 0, 1},
		{"recovers on retry", 2, 3},
		{"gives up after 3 retries", 10, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &fakeSender{failures: tt.failures}
			q := NewQueue(s, slog.New(slog.NewTextHandler(io.Discard, nil)), 10, 3, time.Millisecond)
			q.Start(1)
			if !q.Enqueue(Message{Subject: "s", Body: "b"}) {
				t.Fatal("enqueue rejected")
			}
			q.Close() // ждёт воркер, после этого s.calls безопасно читать
			if s.calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", s.calls, tt.wantCalls)
			}
		})
	}
}

func TestEnqueueAfterCloseRejected(t *testing.T) {
	q := NewQueue(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)), 1, 0, 0)
	q.Start(1)
	q.Close()
	if q.Enqueue(Message{}) {
		t.Fatal("enqueue after close must return false")
	}
}
