package main

import (
	"errors"
	"fmt"
	"time"

	tencentcloud_cls_sdk_go "github.com/tencentcloud/tencentcloud-cls-sdk-go"
	"go.uber.org/ratelimit"
	"go.uber.org/zap"
)

type ClientConfig struct {
	Endpoint  string
	SecretID  string
	SecretKey string
	TopicID   string

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
	cfg              ClientConfig
	producerInstance *tencentcloud_cls_sdk_go.AsyncProducerClient
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

	return &Client{
		cfg:              cfg,
		producerInstance: producerInstance,
	}, nil
}

// SendMessage sends a message to a Tencent CLS.
func (c *Client) SendMessage(text string) error {
	log := tencentcloud_cls_sdk_go.NewCLSLog(time.Now().Unix(), map[string]string{"content": text})
	err := c.producerInstance.SendLog(c.cfg.TopicID, log, callback)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

var callback = &clsCallback{}

type clsCallback struct {
}

func (callback *clsCallback) Success(result *tencentcloud_cls_sdk_go.Result) {
	attemptList := result.GetReservedAttempts()
	for _, attempt := range attemptList {
		fmt.Printf("%+v \n", attempt)
	}
}
func (callback *clsCallback) Fail(result *tencentcloud_cls_sdk_go.Result) {
	fmt.Println(result.IsSuccessful())
	fmt.Println(result.GetErrorCode())
	fmt.Println(result.GetErrorMessage())
	fmt.Println(result.GetReservedAttempts())
	fmt.Println(result.GetRequestId())
	fmt.Println(result.GetTimeStampMs())
}
