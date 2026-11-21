package handler

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

// IPLimiter — лимит на IP: perMinute запросов в минуту, с всплеском burst.
type IPLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
	limit   rate.Limit
	burst   int
	sweepAt time.Time
}

func NewIPLimiter(perMinute, burst int) *IPLimiter {
	return &IPLimiter{
		entries: make(map[string]*ipEntry),
		limit:   rate.Every(time.Minute / time.Duration(perMinute)),
		burst:   burst,
	}
}

func (l *IPLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.After(l.sweepAt) { // чистим давно не обращавшиеся IP, чтобы карта не росла бесконечно
		for k, e := range l.entries {
			if now.Sub(e.seen) > 10*time.Minute {
				delete(l.entries, k)
			}
		}
		l.sweepAt = now.Add(time.Minute)
	}

	e, ok := l.entries[ip]
	if !ok {
		e = &ipEntry{lim: rate.NewLimiter(l.limit, l.burst)}
		l.entries[ip] = e
	}
	e.seen = now
	return e.lim.AllowN(now, 1)
}

// clientIP берёт IP клиента. За прокси (Caddy) добавленный им последний адрес в
// X-Forwarded-For — настоящий клиент; первый адрес клиент может подделать.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
