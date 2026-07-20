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
	mu          sync.Mutex
	dlqCalls    int
	dlqErr      error
	retryCalls  int
	retryErr    error
	lastAttempt int
	lastExp     time.Duration
}

func (m *mockDeadLetterPublisher) PublishDeadLetter(ctx context.Context, job RefreshJob, reason string, attempt int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dlqCalls++
	m.lastAttempt = attempt
	return m.dlqErr
}

func (m *mockDeadLetterPublisher) PublishRetry(ctx context.Context, job RefreshJob, attempt int, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryCalls++
	m.lastAttempt = attempt
	m.lastExp = expiration
	return m.retryErr
}

func testConsumer(pub deliveryPublisher, handler Handler) *Consumer {
	return &Consumer{
		client:     &Client{cfg: Config{Queue: "repo.refresh"}},
		maxRetries: 3,
		publisher:  pub,
		handler:    handler,
		backoff: func(attempt int, retryAfter time.Duration) time.Duration {
			if retryAfter > 0 {
				return retryAfter
			}
			return time.Millisecond
		},
	}
}

func TestHandleDelivery_DLQPublishFailureNacks(t *testing.T) {
	job := RefreshJob{JobID: uuid.New(), BatchID: uuid.New(), RepoID: uuid.New()}
	body, err := job.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ack := &mockAcknowledger{}
	pub := &mockDeadLetterPublisher{dlqErr: errors.New("dlq unavailable")}
	c := testConsumer(pub, func(ctx context.Context, job RefreshJob, attempt int) error {
		return Permanent("not found")
	})

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
	c := testConsumer(pub, func(ctx context.Context, job RefreshJob, attempt int) error {
		return Permanent("not found")
	})

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
	c := testConsumer(pub, func(ctx context.Context, job RefreshJob, attempt int) error {
		return NewRateLimited(errors.New("rate limited"), 50*time.Millisecond)
	})

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
	if pub.retryCalls != 1 {
		t.Fatalf("retry calls = %d, want 1", pub.retryCalls)
	}
	if pub.lastAttempt != 3 {
		t.Fatalf("retry attempt = %d, want 3 (unchanged)", pub.lastAttempt)
	}
}

func TestHandleDelivery_TransientRoutesToRetryWithoutSleep(t *testing.T) {
	job := RefreshJob{JobID: uuid.New(), BatchID: uuid.New(), RepoID: uuid.New()}
	body, err := job.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ack := &mockAcknowledger{}
	pub := &mockDeadLetterPublisher{}
	started := time.Now()
	c := testConsumer(pub, func(ctx context.Context, job RefreshJob, attempt int) error {
		return NewTransient(errors.New("server error"), 0)
	})

	d := amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  1,
		Body:         body,
		Headers:      amqp.Table{HeaderAttempt: int32(1)},
	}
	c.handleDelivery(context.Background(), context.Background(), d)
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("handleDelivery slept; expected immediate retry publish")
	}

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.acks != 1 {
		t.Fatalf("acks = %d, want 1", ack.acks)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if pub.retryCalls != 1 {
		t.Fatalf("retry calls = %d, want 1", pub.retryCalls)
	}
	if pub.lastAttempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", pub.lastAttempt)
	}
}

func TestHandleDelivery_RetryPublishFailureNacks(t *testing.T) {
	job := RefreshJob{JobID: uuid.New(), BatchID: uuid.New(), RepoID: uuid.New()}
	body, err := job.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ack := &mockAcknowledger{}
	pub := &mockDeadLetterPublisher{retryErr: errors.New("retry unavailable")}
	c := testConsumer(pub, func(ctx context.Context, job RefreshJob, attempt int) error {
		return NewTransient(errors.New("network"), 0)
	})

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
	if ack.nacks != 1 || !ack.requeue {
		t.Fatalf("nacks=%d requeue=%v, want nack requeue", ack.nacks, ack.requeue)
	}
}
