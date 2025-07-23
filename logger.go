package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/docker/docker/daemon/logger"
	"github.com/valyala/fasttemplate"
	"go.uber.org/zap"
)

const (
	// driverName is the name of the driver.
	driverName = "tencent-cls"

	// defaultBufferCapacity is the default buffer capacity of the logger.
	defaultBufferCapacity = 10_000
)

var (
	errUnknownTag   = errors.New("unknown tag")
	errLoggerClosed = errors.New("logger is closed")
)

// client is an interface that represents a Tencent CLS client.
type client interface {
	SendMessage(message string) error
	Close() error
}

// TencentCLSLoggerOption is a function that configures a TencentCLSLogger.
type TencentCLSLoggerOption func(*TencentCLSLogger)

// TencentCLSLogger is a logger that sends logs to Tencent CLS.
// It implements the logger.Logger interface.
type TencentCLSLogger struct {
	client client

	formatter *messageFormatter
	cfg       *loggerConfig

	buffer chan string
	mu     sync.Mutex

	partialLogsBuffer *partialLogBuffer

	wg     sync.WaitGroup
	closed chan struct{}
	logger *zap.Logger
}

var _ = (logger.Logger)(&TencentCLSLogger{})

// NewTencentCLSLogger creates a new TencentCLSLogger.
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
		return nil, fmt.Errorf("failed to create Tencent CLS Client: %w", err)
	}

	l := &TencentCLSLogger{
		client:            client,
		formatter:         formatter,
		cfg:               cfg,
		partialLogsBuffer: newPartialLogBuffer(),
		closed:            make(chan struct{}),
		logger:            logger,
	}

	for _, opt := range opts {
		opt(l)
	}

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
	l.send(text)
	return nil
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

	if err := l.client.Close(); err != nil {
		l.logger.Error("failed to close Tencent CLS Client", zap.Error(err))
	}

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
