package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	tencentcloud_cls_sdk_go "github.com/tencentcloud/tencentcloud-cls-sdk-go"
	"go.uber.org/ratelimit"
	"go.uber.org/zap"
)

type ClientConfig struct {
	Endpoint     string
	SecretID     string
	SecretKey    string
	TopicID      string
	InstanceInfo string

	// Retries is the number of retries to call the Tencent CLS API.
	Retries int

	// Timeout is the timeout for the HTTP Client.
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	Timeout time.Duration
}

func (c ClientConfig) Validate() error {
	var errs []error

	if c.Endpoint == "" {
		errs = append(errs, errors.New("endpoint is required"))
	}
	if c.SecretID == "" {
		errs = append(errs, errors.New("secret ID is required"))
	}
	if c.SecretKey == "" {
		errs = append(errs, errors.New("secret key is required"))
	}
	if c.TopicID == "" {
		errs = append(errs, errors.New("topic ID is required"))
	}

	return errors.Join(errs...)
}

// Client is a Tencent CLS client.
// It is used to send messages to a Tencent CLS topic.
type Client struct {
	logger   *zap.Logger
	cfg      ClientConfig
	producer *tencentcloud_cls_sdk_go.AsyncProducerClient
	callback *clsCallback
}

// NewClient creates a new Tencent CLS client.
func NewClient(logger *zap.Logger, cfg ClientConfig, limiterOpts ...ratelimit.Option) (*Client, error) {
	producerConfig := tencentcloud_cls_sdk_go.GetDefaultAsyncProducerClientConfig()
	producerConfig.Endpoint = cfg.Endpoint
	producerConfig.AccessKeyID = cfg.SecretID
	producerConfig.AccessKeySecret = cfg.SecretKey
	producerConfig.Timeout = int(cfg.Timeout.Milliseconds())
	producerConfig.Retries = cfg.Retries

	// 设置要上传日志的主题 ID，替换为您的 Topic ID
	// 创建异步生产者客户端实例
	producerInstance, err := tencentcloud_cls_sdk_go.NewAsyncProducerClient(producerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Tencent CLS Client: %w", err)
	}
	producerInstance.Start()

	return &Client{
		logger:   logger,
		cfg:      cfg,
		producer: producerInstance,
		callback: &clsCallback{
			logger: logger,
		},
	}, nil
}

// SendMessage sends a message to a Tencent CLS.
func (c *Client) SendMessage(text string) error {
	addLogMap := map[string]string{}
	if err := json.Unmarshal([]byte(text), &addLogMap); err != nil {
		c.logger.Debug("failed to unmarshal log", zap.String("log", text), zap.Error(err))
		addLogMap["content"] = text
	}

	if c.cfg.InstanceInfo != "" {
		instanceInfo := map[string]string{}
		if err := json.Unmarshal([]byte(c.cfg.InstanceInfo), &instanceInfo); err != nil {
			c.logger.Debug("failed to unmarshal instance info", zap.String("instanceInfo", c.cfg.InstanceInfo), zap.Error(err))
			addLogMap["instance"] = c.cfg.InstanceInfo
		} else {
			for k, v := range instanceInfo {
				addLogMap["__instance__."+k] = v
			}
		}
	}

	log := tencentcloud_cls_sdk_go.NewCLSLog(time.Now().Unix(), addLogMap)
	err := c.producer.SendLog(c.cfg.TopicID, log, c.callback)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (c *Client) Close() error {
	return c.producer.Close(60000)
}

type clsCallback struct {
	logger *zap.Logger
}

func (callback *clsCallback) Success(result *tencentcloud_cls_sdk_go.Result) {
	callback.logger.Debug("cls callback success", zap.Any("attempts", result.GetReservedAttempts()))
}
func (callback *clsCallback) Fail(result *tencentcloud_cls_sdk_go.Result) {
	callback.logger.Error("cls callback fail",
		zap.Any("isSuccessful", result.IsSuccessful()),
		zap.Any("errorCode", result.GetErrorCode()),
		zap.Any("errorMessage", result.GetErrorMessage()),
		zap.Any("attempts", result.GetReservedAttempts()),
		zap.Any("requestId", result.GetRequestId()),
		zap.Any("timeStampMs", result.GetTimeStampMs()),
	)
}
