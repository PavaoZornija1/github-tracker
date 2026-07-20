package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// deliveryPublisher is the subset of Publisher used by the consumer for
// retry and dead-letter paths (injected for unit tests).
type deliveryPublisher interface {
	PublishDeadLetter(ctx context.Context, job RefreshJob, reason string, attempt int) error
	RepublishRefresh(ctx context.Context, job RefreshJob, attempt int) error
}

// Config holds RabbitMQ topology names.
type Config struct {
	URL      string
	Exchange string
	Queue    string
	DLX      string
	DLQ      string
}

// Client owns the AMQP connection and declared topology.
type Client struct {
	cfg  Config
	conn *amqp.Connection
	mu   sync.Mutex
}

// Connect dials RabbitMQ and declares durable topology.
func Connect(cfg Config) (*Client, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	c := &Client{cfg: cfg, conn: conn}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()
	if err := c.declare(ch); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) declare(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(c.cfg.Exchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(c.cfg.DLX, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx: %w", err)
	}
	if _, err := ch.QueueDeclare(c.cfg.Queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	if _, err := ch.QueueDeclare(c.cfg.DLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq: %w", err)
	}
	if err := ch.QueueBind(c.cfg.Queue, RoutingKeyRefresh, c.cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("bind queue: %w", err)
	}
	if err := ch.QueueBind(c.cfg.DLQ, "dead", c.cfg.DLX, false, nil); err != nil {
		return fmt.Errorf("bind dlq: %w", err)
	}
	return nil
}

// Close closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Publisher publishes refresh jobs to the main exchange.
type Publisher struct {
	client *Client
}

func NewPublisher(client *Client) *Publisher {
	return &Publisher{client: client}
}

// PublishRefresh enqueues one refresh job (attempt starts at 1).
func (p *Publisher) PublishRefresh(ctx context.Context, job RefreshJob) error {
	return p.publish(ctx, p.client.cfg.Exchange, RoutingKeyRefresh, job, 1)
}

func (p *Publisher) publish(ctx context.Context, exchange, key string, job RefreshJob, attempt int) error {
	body, err := job.Marshal()
	if err != nil {
		return err
	}
	ch, err := p.client.conn.Channel()
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	defer ch.Close()

	return ch.PublishWithContext(ctx, exchange, key, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Headers:      amqp.Table{HeaderAttempt: int32(attempt)},
		Body:         body,
	})
}

// PublishDeadLetter parks a permanently failed job on the DLQ exchange.
func (p *Publisher) PublishDeadLetter(ctx context.Context, job RefreshJob, reason string, attempt int) error {
	body, err := job.Marshal()
	if err != nil {
		return err
	}
	ch, err := p.client.conn.Channel()
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	defer ch.Close()

	return ch.PublishWithContext(ctx, p.client.cfg.DLX, "dead", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Headers: amqp.Table{
			HeaderAttempt: int32(attempt),
			"x-reason":    reason,
		},
		Body: body,
	})
}

// RepublishRefresh re-queues a job after a transient failure with an incremented attempt.
func (p *Publisher) RepublishRefresh(ctx context.Context, job RefreshJob, attempt int) error {
	return p.publish(ctx, p.client.cfg.Exchange, RoutingKeyRefresh, job, attempt)
}

// Handler processes one delivery. attempt is 1-based from message headers.
type Handler func(ctx context.Context, job RefreshJob, attempt int) error

// Consumer pulls jobs with bounded prefetch and manual ack.
type Consumer struct {
	client      *Client
	concurrency int
	maxRetries  int
	publisher   deliveryPublisher
	handler     Handler
	backoff     func(attempt int, retryAfter time.Duration) time.Duration
}

// ConsumerOptions configures the worker consumer.
type ConsumerOptions struct {
	Concurrency int
	MaxRetries  int
	Handler     Handler
	Backoff     func(attempt int, retryAfter time.Duration) time.Duration
}

// NewConsumer builds a consumer. Handler should return:
//   - nil on success / already-done
//   - *TransientError to retry
//   - any other error as permanent failure → DLQ
func NewConsumer(client *Client, opts ConsumerOptions) *Consumer {
	backoff := opts.Backoff
	if backoff == nil {
		backoff = DefaultBackoff
	}
	return &Consumer{
		client:      client,
		concurrency: opts.Concurrency,
		maxRetries:  opts.MaxRetries,
		publisher:   NewPublisher(client),
		handler:     opts.Handler,
		backoff:     backoff,
	}
}

// Run consumes until ctx is cancelled, then waits for in-flight handlers.
func (c *Consumer) Run(ctx context.Context) error {
	ch, err := c.client.conn.Channel()
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	defer ch.Close()

	if err := ch.Qos(c.concurrency, 0, false); err != nil {
		return fmt.Errorf("qos: %w", err)
	}

	deliveries, err := ch.Consume(c.client.cfg.Queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	sem := make(chan struct{}, c.concurrency)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case d, ok := <-deliveries:
			if !ok {
				wg.Wait()
				return fmt.Errorf("delivery channel closed")
			}
			sem <- struct{}{}
			wg.Add(1)
			go func(del amqp.Delivery) {
				defer wg.Done()
				defer func() { <-sem }()
				// Finish in-flight work even if the consumer context was cancelled.
				jobCtx := context.WithoutCancel(ctx)
				c.handleDelivery(jobCtx, ctx, del)
			}(d)
		}
	}
}

func (c *Consumer) handleDelivery(jobCtx, shutdownCtx context.Context, d amqp.Delivery) {
	job, err := UnmarshalRefreshJob(d.Body)
	if err != nil {
		c.deadLetterOrNack(jobCtx, d, RefreshJob{}, "invalid_payload", 1)
		return
	}

	attempt := headerAttempt(d.Headers)
	err = c.handler(jobCtx, job, attempt)
	if err == nil {
		_ = d.Ack(false)
		return
	}

	var transient *TransientError
	if AsTransient(err, &transient) {
		if transient.CountAsAttempt && attempt >= c.maxRetries {
			c.deadLetterOrNack(jobCtx, d, job, transient.Error(), attempt)
			return
		}
		delay := c.backoff(attempt, transient.RetryAfter)
		timer := time.NewTimer(delay)
		select {
		case <-shutdownCtx.Done():
			timer.Stop()
			_ = d.Nack(false, true)
			return
		case <-timer.C:
		}
		nextAttempt := attempt
		if transient.CountAsAttempt {
			nextAttempt = attempt + 1
		}
		if pubErr := c.publisher.RepublishRefresh(jobCtx, job, nextAttempt); pubErr != nil {
			_ = d.Nack(false, true)
			return
		}
		_ = d.Ack(false)
		return
	}

	c.deadLetterOrNack(jobCtx, d, job, err.Error(), attempt)
}

// deadLetterOrNack publishes to the DLQ then Acks; on publish failure Nacks with requeue.
func (c *Consumer) deadLetterOrNack(ctx context.Context, d amqp.Delivery, job RefreshJob, reason string, attempt int) {
	if err := c.publisher.PublishDeadLetter(ctx, job, reason, attempt); err != nil {
		slog.Error("dead-letter publish failed; nacking for requeue",
			"err", err,
			"job_id", job.JobID,
			"attempt", attempt,
			"reason", reason,
		)
		_ = d.Nack(false, true)
		return
	}
	_ = d.Ack(false)
}

func headerAttempt(h amqp.Table) int {
	if h == nil {
		return 1
	}
	v, ok := h[HeaderAttempt]
	if !ok {
		return 1
	}
	switch n := v.(type) {
	case int32:
		if n > 0 {
			return int(n)
		}
	case int64:
		if n > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	}
	return 1
}
