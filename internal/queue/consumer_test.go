package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type mockAcknowledger struct {
	mu      sync.Mutex
	acks    int
	nacks   int
	requeue bool
}

func (m *mockAcknowledger) Ack(tag uint64, multiple bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acks++
	return nil
}

func (m *mockAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nacks++
	m.requeue = requeue
	return nil
}

func (m *mockAcknowledger) Reject(tag uint64, requeue bool) error {
	return m.Nack(tag, false, requeue)
}

type mockDeadLetterPublisher struct {
	mu         sync.Mutex
	dlqCalls   int
	dlqErr     error
	repCalls   int
	repErr     error
	lastAttempt int
}

func (m *mockDeadLetterPublisher) PublishDeadLetter(ctx context.Context, job RefreshJob, reason string, attempt int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dlqCalls++
	m.lastAttempt = attempt
	return m.dlqErr
}

func (m *mockDeadLetterPublisher) RepublishRefresh(ctx context.Context, job RefreshJob, attempt int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repCalls++
	m.lastAttempt = attempt
	return m.repErr
}

func TestHandleDelivery_DLQPublishFailureNacks(t *testing.T) {
	job := RefreshJob{JobID: uuid.New(), BatchID: uuid.New(), RepoID: uuid.New()}
	body, err := job.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ack := &mockAcknowledger{}
	pub := &mockDeadLetterPublisher{dlqErr: errors.New("dlq unavailable")}
	c := &Consumer{
		maxRetries: 3,
		publisher:  pub,
		handler: func(ctx context.Context, job RefreshJob, attempt int) error {
			return Permanent("not found")
		},
		backoff: DefaultBackoff,
	}

	d := amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  1,
		Body:         body,
		Headers:      amqp.Table{HeaderAttempt: int32(1)},
	}
	c.handleDelivery(context.Background(), context.Background(), d)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.acks != 0 {
		t.Fatalf("acks = %d, want 0", ack.acks)
	}
	if ack.nacks != 1 {
		t.Fatalf("nacks = %d, want 1", ack.nacks)
	}
	if !ack.requeue {
		t.Fatal("expected requeue=true on nack")
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if pub.dlqCalls != 1 {
		t.Fatalf("dlq calls = %d, want 1", pub.dlqCalls)
	}
}

func TestHandleDelivery_DLQPublishSuccessAcks(t *testing.T) {
	job := RefreshJob{JobID: uuid.New(), BatchID: uuid.New(), RepoID: uuid.New()}
	body, err := job.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ack := &mockAcknowledger{}
	pub := &mockDeadLetterPublisher{}
	c := &Consumer{
		maxRetries: 3,
		publisher:  pub,
		handler: func(ctx context.Context, job RefreshJob, attempt int) error {
			return Permanent("not found")
		},
		backoff: DefaultBackoff,
	}

	d := amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  1,
		Body:         body,
		Headers:      amqp.Table{HeaderAttempt: int32(1)},
	}
	c.handleDelivery(context.Background(), context.Background(), d)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.acks != 1 {
		t.Fatalf("acks = %d, want 1", ack.acks)
	}
	if ack.nacks != 0 {
		t.Fatalf("nacks = %d, want 0", ack.nacks)
	}
}

func TestHandleDelivery_RateLimitKeepsSameAttempt(t *testing.T) {
	job := RefreshJob{JobID: uuid.New(), BatchID: uuid.New(), RepoID: uuid.New()}
	body, err := job.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ack := &mockAcknowledger{}
	pub := &mockDeadLetterPublisher{}
	c := &Consumer{
		maxRetries: 3,
		publisher:  pub,
		handler: func(ctx context.Context, job RefreshJob, attempt int) error {
			return NewRateLimited(errors.New("rate limited"), time.Millisecond)
		},
		backoff: func(attempt int, retryAfter time.Duration) time.Duration {
			return time.Millisecond
		},
	}

	d := amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  1,
		Body:         body,
		Headers:      amqp.Table{HeaderAttempt: int32(3)},
	}
	c.handleDelivery(context.Background(), context.Background(), d)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.acks != 1 {
		t.Fatalf("acks = %d, want 1", ack.acks)
	}
	if ack.nacks != 0 {
		t.Fatalf("nacks = %d, want 0 (must not DLQ rate-limit at maxRetries)", ack.nacks)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if pub.dlqCalls != 0 {
		t.Fatalf("dlq calls = %d, want 0", pub.dlqCalls)
	}
	if pub.repCalls != 1 {
		t.Fatalf("republish calls = %d, want 1", pub.repCalls)
	}
	if pub.lastAttempt != 3 {
		t.Fatalf("republish attempt = %d, want 3 (unchanged)", pub.lastAttempt)
	}
}
