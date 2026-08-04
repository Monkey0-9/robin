package main

import (
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type UserRateLimit struct {
	count     int64
	resetTime time.Time
}

var rateLimitStore sync.Map

// RateLimitMiddleware applies a sliding window rate limit per user ID in-memory.
func RateLimitMiddleware(next http.Handler, limit int, window time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			userID = r.RemoteAddr
		}

		now := time.Now()
		val, loaded := rateLimitStore.Load(userID)

		var entry *UserRateLimit
		if !loaded || now.After(val.(*UserRateLimit).resetTime) {
			entry = &UserRateLimit{
				count:     1,
				resetTime: now.Add(window),
			}
			rateLimitStore.Store(userID, entry)
		} else {
			entry = val.(*UserRateLimit)
			atomic.AddInt64(&entry.count, 1)
		}

		if atomic.LoadInt64(&entry.count) > int64(limit) {
			log.Printf("[RATE LIMIT] User %s exceeded limit of %d", userID, limit)
			http.Error(w, "Too Many Requests - Rate Limit Exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
