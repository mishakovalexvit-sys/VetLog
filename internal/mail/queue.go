package mail

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Queue принимает письма через канал, отправляет их в фоновых горутинах
// и повторяет неудачные попытки с экспоненциальной паузой.
type Queue struct {
	ch      chan Message
	sender  Sender
	log     *slog.Logger
	retries int
	backoff time.Duration

	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
}

func NewQueue(s Sender, log *slog.Logger, size, retries int, backoff time.Duration) *Queue {
	return &Queue{
		ch:      make(chan Message, size),
		sender:  s,
		log:     log,
		retries: retries,
		backoff: backoff,
	}
}

// Start запускает воркеры.
func (q *Queue) Start(workers int) {
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.work()
	}
}

// Enqueue не блокирует: при переполнении или после Close возвращает false.
func (q *Queue) Enqueue(m Message) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return false
	}
	select {
	case q.ch <- m:
		return true
	default:
		return false
	}
}

// Close перестаёт принимать письма, дожидается отправки уже поставленных.
func (q *Queue) Close() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.ch)
	}
	q.mu.Unlock()
	q.wg.Wait()
}

func (q *Queue) work() {
	defer q.wg.Done()
	for m := range q.ch {
		q.deliver(m)
	}
}

// deliver делает 1 попытку и до retries повторов: паузы backoff, 2*backoff, 4*backoff...
func (q *Queue) deliver(m Message) {
	delay := q.backoff
	for attempt := 0; ; attempt++ {
		err := q.sender.Send(context.Background(), m)
		if err == nil {
			q.log.Info("mail sent", slog.Int("attempts", attempt+1))
			return
		}
		if attempt >= q.retries {
			q.log.Error("mail delivery failed", slog.Int("attempts", attempt+1), slog.Any("err", err))
			return
		}
		q.log.Warn("mail retry scheduled", slog.Int("attempt", attempt+1),
			slog.Duration("delay", delay), slog.Any("err", err))
		time.Sleep(delay)
		delay *= 2
	}
}
