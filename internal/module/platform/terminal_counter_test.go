package platform

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTerminalCounterCachesWithinTTL(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	c := NewTerminalCounter(func(context.Context) (int64, error) {
		calls++
		return 42, nil
	}, time.Minute)
	c.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		v, err := c.Count(context.Background())
		if err != nil || v != 42 {
			t.Fatalf("count=%d err=%v", v, err)
		}
	}
	if calls != 1 {
		t.Fatalf("TTL 内应只查询一次，实际 %d", calls)
	}
	now = now.Add(61 * time.Second)
	if _, _ = c.Count(context.Background()); calls != 2 {
		t.Fatalf("TTL 过期后应重新查询，实际 %d", calls)
	}
}

func TestTerminalCounterFallsBackOnError(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	failing := false
	c := NewTerminalCounter(func(context.Context) (int64, error) {
		if failing {
			return 0, errors.New("ch down")
		}
		return 7, nil
	}, time.Minute)
	c.now = func() time.Time { return now }

	if v, _ := c.Count(context.Background()); v != 7 {
		t.Fatalf("first=%d", v)
	}
	now = now.Add(2 * time.Minute)
	failing = true
	if v, err := c.Count(context.Background()); err != nil || v != 7 {
		t.Fatalf("降级应返回上次值: v=%d err=%v", v, err)
	}
}

func TestTerminalCounterWithoutSource(t *testing.T) {
	if _, err := NewTerminalCounter(nil, time.Minute).Count(context.Background()); err == nil {
		t.Fatal("nil 计数源应报错")
	}
	var nilCounter *TerminalCounter
	if _, err := nilCounter.Count(context.Background()); err == nil {
		t.Fatal("nil 接收者应报错")
	}
}
