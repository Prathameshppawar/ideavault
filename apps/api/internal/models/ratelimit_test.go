package models

import (
	"context"
	"testing"
	"time"
)

func TestGatewayWaitsOutPerMinuteLimitsAndSkipsTooLarge(t *testing.T) {
	limited := func(p string) fakeReply {
		return fakeReply{err: &ProviderError{Provider: p, StatusCode: 429, Message: "Rate limit reached on tokens per minute (TPM). Please try again in 0.01s."}}
	}
	// a is rate limited twice, then answers; b rejects the request as too large every time.
	a := &fakeProvider{id: "a", replies: []fakeReply{limited("a"), limited("a"), {content: "answer from a"}}}
	b := &fakeProvider{id: "b", replies: []fakeReply{{err: &ProviderError{Provider: "b", StatusCode: 413, Message: "Request too large on tokens per minute (TPM): Limit 8000, Requested 9502"}}}}
	gw := newTestGateway(nil, a, b)
	var waits []time.Duration
	res, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, Request: Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		OnRetry: func(w time.Duration, _ string) { waits = append(waits, w) }})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if res.Content != "answer from a" || a.numCalls() != 3 {
		t.Fatalf("content=%q a calls=%d, want the third attempt on a to succeed", res.Content, a.numCalls())
	}
	if b.numCalls() != 1 {
		t.Errorf("a model that rejected the request as too large must not be retried; b calls=%d", b.numCalls())
	}
	if len(waits) != 2 {
		t.Errorf("expected two announced waits, got %v", waits)
	}
}

func TestGatewayGivesUpAfterTwoRateLimitRounds(t *testing.T) {
	a := &fakeProvider{id: "a", replies: []fakeReply{{err: &ProviderError{Provider: "a", StatusCode: 429, Message: "Please try again in 0.01s."}}}}
	gw := newTestGateway(nil, a)
	_, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, Request: Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}})
	if err == nil || a.numCalls() != 1+maxRateLimitRetries {
		t.Fatalf("err=%v calls=%d, want failure after %d calls", err, a.numCalls(), 1+maxRateLimitRetries)
	}
}

func TestGatewayDoesNotWaitOnDailyQuota(t *testing.T) {
	a := &fakeProvider{id: "a", replies: []fakeReply{{err: &ProviderError{Provider: "a", StatusCode: 429, Message: "tokens per day (TPD) exceeded. Please try again in 7m12s."}}}}
	gw := newTestGateway(nil, a)
	start := time.Now()
	if _, err := gw.Do(context.Background(), Call{Task: TaskSynthesis, Request: Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}}); err == nil {
		t.Fatal("expected the daily-quota error")
	}
	if a.numCalls() != 1 || time.Since(start) > time.Second {
		t.Errorf("a daily quota must fail fast without retrying (calls=%d)", a.numCalls())
	}
}
