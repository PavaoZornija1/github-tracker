package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// deliveryPublisher is the subset of Publisher used by the consumer for
// retry and dead-letter paths (injected for unit tests).
type deliveryPublisher interface {
	PublishDeadLetter(ctx context.Context, job RefreshJob, reason string, attempt int) error
	PublishRetry(ctx context.Context, job RefreshJob, attempt int, expiration time.Duration) error
}

// Config holds RabbitMQ topology names.
type Config struct {
	URL      string
	Exchange string
	Queue    string
	DLX      string
	DLQ      string
}

// RetryQueueName derives the TTL retry queue from the main work queue.
func RetryQueueName(queue string) string {
	return queue + ".retry"
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

	retryQueue := RetryQueueName(c.cfg.Queue)
	retryArgs := amqp.Table{
		"x-dead-letter-exchange":    c.cfg.Exchange,
		"x-dead-letter-routing-key": RoutingKeyRefresh,
	}
	if _, err := ch.QueueDeclare(retryQueue, true, false, false, false, retryArgs); err != nil {
		return fmt.Errorf("declare retry queue: %w", err)
	}
	if err := ch.QueueBind(retryQueue, RoutingKeyRetry, c.cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("bind retry queue: %w", err)
	}

	if _, err := ch.QueueDeclare(c.cfg.Queue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	if _, err := ch.QueueDeclare(c.cfg.DLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq: %w", err)
	}
	if err := ch.QueueBind(c.cfg.Queue, RoutingKeyRefresh, c.cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("bind refresh: %w", err)
	}
	if err := ch.QueueBind(c.cfg.Queue, RoutingKeyBatchKick, c.cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("bind batch kick: %w", err)
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

const publishConfirmTimeout = 5 * time.Second

// Publisher publishes refresh jobs to the main exchange with publisher confirms.
// A single AMQP channel is reused under mu (channels are not concurrent-safe).
type Publisher struct {
	client   *Client
	mu       sync.Mutex
	ch       *amqp.Channel
	confirms <-chan amqp.Confirmation
}

func NewPublisher(client *Client) *Publisher {
	return &Publisher{client: client}
}

func (p *Publisher) ensureChannel() error {
	if p.ch != nil && !p.ch.IsClosed() {
		return nil
	}
	ch, err := p.client.conn.Channel()
	if err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return fmt.Errorf("confirm mode: %w", err)
	}
	p.ch = ch
	p.confirms = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	return nil
}

func (p *Publisher) publishConfirmed(ctx context.Context, exchange, key string, msg amqp.Publishing) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureChannel(); err != nil {
		return err
	}
	if err := p.ch.PublishWithContext(ctx, exchange, key, false, false, msg); err != nil {
		_ = p.ch.Close()
		p.ch = nil
		return err
	}

	timeout := publishConfirmTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("publish confirm timeout after %s", timeout)
	case conf, ok := <-p.confirms:
		if !ok {
			p.ch = nil
			return fmt.Errorf("confirm channel closed")
		}
		if !conf.Ack {
			return fmt.Errorf("publish nacked by broker")
		}
		return nil
	}
}

// PublishRefresh enqueues one refresh job (attempt starts at 1).
func (p *Publisher) PublishRefresh(ctx context.Context, job RefreshJob) error {
	return p.publishRefresh(ctx, p.client.cfg.Exchange, RoutingKeyRefresh, job, 1, "")
}

// PublishBatchKick enqueues a fan-out kick for a refresh batch.
func (p *Publisher) PublishBatchKick(ctx context.Context, kick BatchKick) error {
	body, err := kick.Marshal()
	if err != nil {
		return err
	}
	return p.publishConfirmed(ctx, p.client.cfg.Exchange, RoutingKeyBatchKick, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Headers:      amqp.Table{HeaderMessageType: MessageTypeBatchKick},
		Body:         body,
	})
}

func (p *Publisher) publishRefresh(ctx context.Context, exchange, key string, job RefreshJob, attempt int, expiration string) error {
	body, err := job.Marshal()
	if err != nil {
		return err
	}
	return p.publishConfirmed(ctx, exchange, key, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Expiration:   expiration,
		Headers: amqp.Table{
			HeaderAttempt:     int32(attempt),
			HeaderMessageType: MessageTypeRefresh,
		},
		Body: body,
	})
}

// PublishDeadLetter parks a permanently failed job on the DLQ exchange.
func (p *Publisher) PublishDeadLetter(ctx context.Context, job RefreshJob, reason string, attempt int) error {
	body, err := job.Marshal()
	if err != nil {
		return err
	}
	return p.publishConfirmed(ctx, p.client.cfg.DLX, "dead", amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Headers: amqp.Table{
			HeaderAttempt:     int32(attempt),
			HeaderMessageType: MessageTypeRefresh,
			"x-reason":        reason,
		},
		Body: body,
	})
}

// PublishRetry parks a transient failure on the TTL retry queue until expiration,
// then dead-letters back to the main refresh routing key.
func (p *Publisher) PublishRetry(ctx context.Context, job RefreshJob, attempt int, expiration time.Duration) error {
	if expiration < time.Millisecond {
		expiration = time.Millisecond
	}
	if expiration > 60*time.Second {
		expiration = 60 * time.Second
	}
	ms := strconv.FormatInt(expiration.Milliseconds(), 10)
	return p.publishRefresh(ctx, p.client.cfg.Exchange, RoutingKeyRetry, job, attempt, ms)
}

// Handler processes one refresh delivery. attempt is 1-based from message headers.
type Handler func(ctx context.Context, job RefreshJob, attempt int) error

// KickHandler fans out pending refresh jobs for a batch kick message.
type KickHandler func(ctx context.Context, kick BatchKick) error

// Consumer pulls jobs with bounded prefetch and manual ack.
type Consumer struct {
	client      *Client
	concurrency int
	maxRetries  int
	publisher   deliveryPublisher
	handler     Handler
	kickHandler KickHandler
	backoff     func(attempt int, retryAfter time.Duration) time.Duration
}

// ConsumerOptions configures the worker consumer.
type ConsumerOptions struct {
	Concurrency int
	MaxRetries  int
	Handler     Handler
	KickHandler KickHandler
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
		kickHandler: opts.KickHandler,
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
	if err := shutdownCtx.Err(); err != nil {
		_ = d.Nack(false, true)
		return
	}
	switch messageType(d.Headers) {
	case MessageTypeBatchKick:
		c.handleKickDelivery(jobCtx, d)
	case MessageTypeRefresh:
		c.handleRefreshDelivery(jobCtx, d)
	default:
		c.handleRefreshDelivery(jobCtx, d)
	}
}

func (c *Consumer) handleKickDelivery(jobCtx context.Context, d amqp.Delivery) {
	kick, err := UnmarshalBatchKick(d.Body)
	if err != nil {
		c.deadLetterOrNack(jobCtx, d, RefreshJob{}, "invalid_kick_payload", 1)
		return
	}
	if c.kickHandler == nil {
		c.deadLetterOrNack(jobCtx, d, RefreshJob{}, "kick_handler_not_configured", 1)
		return
	}
	err = c.kickHandler(jobCtx, kick)
	if err == nil {
		_ = d.Ack(false)
		return
	}
	var transient *TransientError
	if AsTransient(err, &transient) {
		// Kick redelivery recovers mid-loop fan-out; keep original on queue.
		_ = d.Nack(false, true)
		return
	}
	c.deadLetterOrNack(jobCtx, d, RefreshJob{BatchID: kick.BatchID}, err.Error(), 1)
}

func (c *Consumer) handleRefreshDelivery(jobCtx context.Context, d amqp.Delivery) {
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
		deathCount := xDeathCount(d.Headers, c.client.cfg.Queue)
		effectiveAttempt := attempt
		if deathCount > effectiveAttempt {
			effectiveAttempt = deathCount
		}
		if transient.CountAsAttempt && effectiveAttempt >= c.maxRetries {
			c.deadLetterOrNack(jobCtx, d, job, transient.Error(), effectiveAttempt)
			return
		}
		nextAttempt := attempt
		if transient.CountAsAttempt {
			nextAttempt = attempt + 1
		}
		delay := c.backoff(attempt, transient.RetryAfter)
		if pubErr := c.publisher.PublishRetry(jobCtx, job, nextAttempt, delay); pubErr != nil {
			slog.Error("retry publish failed; nacking for requeue",
				"err", pubErr,
				"job_id", job.JobID,
				"attempt", attempt,
			)
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

func messageType(h amqp.Table) string {
	if h == nil {
		return MessageTypeRefresh
	}
	v, ok := h[HeaderMessageType]
	if !ok {
		return MessageTypeRefresh
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return MessageTypeRefresh
	}
	return s
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

// xDeathCount sums counts from x-death entries for the given queue name.
func xDeathCount(h amqp.Table, queue string) int {
	if h == nil {
		return 0
	}
	raw, ok := h["x-death"]
	if !ok {
		return 0
	}
	entries, ok := raw.([]interface{})
	if !ok {
		return 0
	}
	total := 0
	for _, e := range entries {
		table, ok := e.(amqp.Table)
		if !ok {
			continue
		}
		q, _ := table["queue"].(string)
		if q != queue {
			continue
		}
		switch n := table["count"].(type) {
		case int64:
			total += int(n)
		case int32:
			total += int(n)
		case int:
			total += n
		}
	}
	return total
}
