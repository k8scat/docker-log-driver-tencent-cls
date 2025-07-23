package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/docker/docker/daemon/logger"
	"github.com/valyala/fasttemplate"
	"go.uber.org/zap"
)

const (
	// driverName is the name of the driver.
	driverName = "tencent-cls"

	// defaultLogMessageChars is the default maximum number of characters in a log message.
	defaultLogMessageChars = 4096

	// defaultBufferCapacity is the default buffer capacity of the logger.
	defaultBufferCapacity = 10_000
)

var (
	errUnknownTag   = errors.New("unknown tag")
	errLoggerClosed = errors.New("logger is closed")
)

// client is an interface that represents a Telegram client.
type client interface {
	SendMessage(message string) error
}

// TelegramLoggerOption is a function that configures a TelegramLogger.
type TencentCLSLoggerOption func(*TencentCLSLogger)

// WithBufferCapacity sets the buffer capacity of the logger.
func WithBufferCapacity(capacity int) TencentCLSLoggerOption {
	return func(l *TencentCLSLogger) {
		if capacity > 0 {
			l.buffer = make(chan string, capacity)
		}
	}
}

// WithMaxLogMessageChars sets the maximum number of characters in a log message.
func WithMaxLogMessageChars(maxLen int) TencentCLSLoggerOption {
	return func(l *TencentCLSLogger) {
		l.maxLogMessageChars = maxLen
	}
}

// TelegramLogger is a logger that sends logs to Telegram.
// It implements the logger.Logger interface.
type TencentCLSLogger struct {
	client client

	formatter          *messageFormatter
	cfg                *loggerConfig
	maxLogMessageChars int

	buffer chan string
	mu     sync.Mutex

	partialLogsBuffer *partialLogBuffer

	wg     sync.WaitGroup
	closed chan struct{}
	logger *zap.Logger
}

var _ = (logger.Logger)(&TencentCLSLogger{})

// NewTelegramLogger creates a new TelegramLogger.
func NewTencentCLSLogger(
	logger *zap.Logger,
	containerDetails *ContainerDetails,
	opts ...TencentCLSLoggerOption,
) (*TencentCLSLogger, error) {
	cfg, err := parseLoggerConfig(containerDetails)
	if err != nil {
		return nil, fmt.Errorf("failed to parse logger config: %w", err)
	}

	logger.Debug("parsed logger config", zap.Any("config", cfg))
	logger.Debug("parsed container details", zap.Any("details", containerDetails))

	formatter, err := newMessageFormatter(containerDetails, cfg.Attrs, cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("failed to create message formatter: %w", err)
	}

	client, err := NewClient(logger, cfg.ClientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram Client: %w", err)
	}

	bufferCapacity := defaultBufferCapacity
	if cfg.MaxBufferSize <= 0 {
		bufferCapacity = 0
	}
	buffer := make(chan string, bufferCapacity)

	l := &TencentCLSLogger{
		client:             client,
		formatter:          formatter,
		cfg:                cfg,
		maxLogMessageChars: defaultLogMessageChars,
		buffer:             buffer,
		partialLogsBuffer:  newPartialLogBuffer(),
		closed:             make(chan struct{}),
		logger:             logger,
	}

	for _, opt := range opts {
		opt(l)
	}

	l.wg.Add(1)
	runner := l.runImmediate
	if cfg.BatchEnabled {
		runner = l.runBatching
	}
	go runner()

	return l, nil
}

// Name implements the logger.Logger interface.
func (l *TencentCLSLogger) Name() string {
	return driverName
}

// Log implements the logger.Logger interface.
func (l *TencentCLSLogger) Log(log *logger.Message) error {
	if l.isClosed() {
		return errLoggerClosed
	}

	if log.PLogMetaData != nil {
		assembledLog, last := l.partialLogsBuffer.Append(log)
		if !last {
			return nil
		}

		*log = *assembledLog
	}

	if l.cfg.FilterRegex != nil && !l.cfg.FilterRegex.Match(log.Line) {
		l.logger.Debug("message is filtered out by regex", zap.String("regex", l.cfg.FilterRegex.String()))
		return nil
	}

	text := l.formatter.Format(log)
	// Split the message if it exceeds the maximum number of characters.
	if utf8.RuneCountInString(text) > l.maxLogMessageChars {
		runes := []rune(text)
		for len(runes) > 0 {
			end := l.maxLogMessageChars
			if len(runes) < end {
				end = len(runes)
			}
			slog := string(runes[:end])
			runes = runes[end:]
			if err := l.enqueue(slog); err != nil {
				return err
			}
		}
		return nil
	}

	if err := l.enqueue(text); err != nil {
		return err
	}

	return nil
}

func (l *TencentCLSLogger) enqueue(log string) error {
	if l.cfg.MaxBufferSize <= 0 {
		l.buffer <- log // May block.
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	select {
	case l.buffer <- log:
		return nil
	case <-l.closed:
		return errLoggerClosed
	default:
		// Buffer is full.
		select {
		case <-l.buffer:
			// Drop the oldest message.
		default:
			// Buffer was empty.
		}

		// Try to enqueue the new message again.
		select {
		case l.buffer <- log:
			return nil
		case <-l.closed:
			return errLoggerClosed
		default:
			return errors.New("failed to enqueue message after dropping oldest")
		}
	}
}

func (l *TencentCLSLogger) runImmediate() {
	defer l.wg.Done()

	drain := func() {
		for log := range l.buffer {
			l.send(log)
		}
	}
	defer drain()

	for {
		select {
		case log, ok := <-l.buffer:
			if !ok {
				return
			}
			l.send(log)
		case <-l.closed:
			return
		}
	}
}

func (l *TencentCLSLogger) runBatching() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.cfg.BatchFlushInterval)
	defer ticker.Stop()

	var (
		batch          bytes.Buffer
		batchRuneCount int
	)

	maxBytes := 4 * l.maxLogMessageChars // Unicode characters are up to 4 bytes
	batch.Grow(maxBytes)

	flush := func() {
		if batch.Len() == 0 {
			return
		}

		if batch.Bytes()[batch.Len()-1] == '\n' {
			batch.Truncate(batch.Len() - 1)
			batchRuneCount--
		}

		l.send(batch.String())

		batch.Reset()
		batchRuneCount = 0
	}

	add := func(log string) {
		logLength := utf8.RuneCountInString(log) + 1

		batch.WriteString(log)
		batch.WriteByte('\n')
		batchRuneCount += logLength

		if batchRuneCount >= l.maxLogMessageChars {
			flush()
		}
	}

	drain := func() {
		for log := range l.buffer {
			add(log)
		}
	}
	defer drain()
	defer flush()

	for {
		select {
		case log, ok := <-l.buffer:
			if !ok {
				return
			}
			add(log)
		case <-ticker.C:
			flush()
		case <-l.closed:
			return
		}
	}
}

func (l *TencentCLSLogger) send(log string) {
	if err := l.client.SendMessage(log); err != nil {
		l.logger.Error("failed to send log message", zap.Error(err))
	}
}

// Close implements the logger.Logger interface.
func (l *TencentCLSLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.isClosed() {
		return nil
	}
	close(l.closed)
	close(l.buffer)

	l.wg.Wait()

	return nil
}

func (l *TencentCLSLogger) isClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

// messageFormatter is a helper struct that formats log messages.
type messageFormatter struct {
	template *fasttemplate.Template

	containerDetails *ContainerDetails
	attrs            map[string]string
}

// newMessageFormatter creates a new messageFormatter.
func newMessageFormatter(containerDetails *ContainerDetails, attrs map[string]string, template string) (*messageFormatter, error) {
	t, err := fasttemplate.NewTemplate(template, "{", "}")
	if err != nil {
		return nil, err
	}

	formatter := &messageFormatter{
		template:         t,
		containerDetails: containerDetails,
		attrs:            attrs,
	}

	if err := formatter.validateTemplate(); err != nil {
		return nil, err
	}

	return formatter, nil
}

// Format formats the given message.
func (f *messageFormatter) Format(msg *logger.Message) string {
	return f.template.ExecuteFuncString(f.tagFunc(msg))
}

// validateTemplate validates the template.
func (f *messageFormatter) validateTemplate() error {
	msg := &logger.Message{
		Line:      []byte("validate"),
		Timestamp: time.Now(),
	}
	_, err := f.template.ExecuteFuncStringWithErr(f.tagFunc(msg))
	return err
}

// tagFunc is a fasttemplate.TagFunc that replaces tags with values.
func (f *messageFormatter) tagFunc(msg *logger.Message) fasttemplate.TagFunc {
	return func(w io.Writer, tag string) (int, error) {
		switch tag {
		case "log":
			return w.Write(msg.Line)
		case "timestamp":
			return w.Write([]byte(msg.Timestamp.UTC().Format(time.RFC3339)))
		case "container_id":
			return w.Write([]byte(f.containerDetails.ID()))
		case "container_full_id":
			return w.Write([]byte(f.containerDetails.ContainerID))
		case "container_name":
			return w.Write([]byte(f.containerDetails.Name()))
		case "image_id":
			return w.Write([]byte(f.containerDetails.ImageID()))
		case "image_full_id":
			return w.Write([]byte(f.containerDetails.ContainerImageID))
		case "image_name":
			return w.Write([]byte(f.containerDetails.ImageName()))
		case "daemon_name":
			return w.Write([]byte(f.containerDetails.DaemonName))
		}

		if value, ok := f.attrs[tag]; ok {
			return w.Write([]byte(value))
		}

		return 0, fmt.Errorf("%w: %s", errUnknownTag, tag)
	}
}

type partialLogBuffer struct {
	logs map[string]*logger.Message
	mu   sync.Mutex
}

func newPartialLogBuffer() *partialLogBuffer {
	return &partialLogBuffer{
		logs: map[string]*logger.Message{},
	}
}

func (b *partialLogBuffer) Append(log *logger.Message) (*logger.Message, bool) {
	if log.PLogMetaData == nil {
		panic("log must be partial")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	plog, exists := b.logs[log.PLogMetaData.ID]
	if !exists {
		plog = new(logger.Message)
		*plog = *log

		b.logs[plog.PLogMetaData.ID] = plog

		plog.Line = make([]byte, 0, 16*1024) // 16KB. Arbitrary size
		plog.PLogMetaData = nil
	}

	plog.Line = append(plog.Line, log.Line...)

	if log.PLogMetaData.Last {
		delete(b.logs, log.PLogMetaData.ID)
		return plog, true
	}

	return nil, false
}
