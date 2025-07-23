package main

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

const (
	cfgEndpointKey  = "endpoint"
	cfgSecretIDKey  = "secret_id"
	cfgSecretKeyKey = "secret_key"
	cfgTopicIDKey   = "topic_id"
	cfgRetriesKey   = "retries"
	cfgTimeoutKey   = "timeout"

	cfgNoFileKey   = "no-file"
	cfgKeepFileKey = "keep-file"

	cfgTemplateKey    = "template"
	cfgFilterRegexKey = "filter-regex"
)

type loggerConfig struct {
	ClientConfig ClientConfig

	Attrs map[string]string

	Template    string
	FilterRegex *regexp.Regexp

	MaxBufferSize int64

	BatchFlushInterval time.Duration
}

var defaultLoggerConfig = loggerConfig{
	Template:           "{log}",
	BatchFlushInterval: 3 * time.Second,
	MaxBufferSize:      1e6, // 1MB
}

var defaultClientConfig = ClientConfig{
	Retries: 5,
	Timeout: 10 * time.Second,
}

func parseLoggerConfig(containerDetails *ContainerDetails) (*loggerConfig, error) {
	clientConfig, err := parseClientConfig(containerDetails)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client config: %w", err)
	}
	attrs, err := containerDetails.ExtraAttributes(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse extra attributes: %w", err)
	}

	cfg := defaultLoggerConfig
	cfg.ClientConfig = clientConfig
	cfg.Attrs = attrs

	if template, ok := containerDetails.Config[cfgTemplateKey]; ok {
		cfg.Template = template
	}

	if filterRegex, ok := containerDetails.Config[cfgFilterRegexKey]; ok {
		cfg.FilterRegex, err = regexp.Compile(filterRegex)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %q option: %w", cfgFilterRegexKey, err)
		}
	}

	if err := cfg.Validate(containerDetails.Config); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c loggerConfig) Validate(opts map[string]string) error {
	if err := validateDriverOptions(opts); err != nil {
		return err
	}
	return c.ClientConfig.Validate()
}

func validateDriverOptions(opts map[string]string) error {
	for opt := range opts {
		switch opt {
		case cfgEndpointKey,
			cfgSecretIDKey,
			cfgSecretKeyKey,
			cfgTopicIDKey,
			cfgRetriesKey,
			cfgTimeoutKey,
			cfgTemplateKey,
			cfgFilterRegexKey:
		case "max-file", "max-size", "compress", "labels", "labels-regex", "env", "env-regex", "tag", "mode":
		case cfgNoFileKey, cfgKeepFileKey:
		default:
			return fmt.Errorf("unknown log opt '%s' for tencent cls log driver", opt)
		}
	}

	return nil
}

func parseClientConfig(containerDetails *ContainerDetails) (ClientConfig, error) {
	clientConfig := ClientConfig{
		Endpoint:  containerDetails.Config[cfgEndpointKey],
		SecretID:  containerDetails.Config[cfgSecretIDKey],
		SecretKey: containerDetails.Config[cfgSecretKeyKey],
		TopicID:   containerDetails.Config[cfgTopicIDKey],
		Retries:   defaultClientConfig.Retries,
		Timeout:   defaultClientConfig.Timeout,
	}

	if retries, ok := containerDetails.Config[cfgRetriesKey]; ok {
		var err error
		clientConfig.Retries, err = strconv.Atoi(retries)
		if err != nil {
			return clientConfig, fmt.Errorf("failed to parse %q option: %w", cfgRetriesKey, err)
		}
		if clientConfig.Retries < 0 {
			return clientConfig, fmt.Errorf("invalid %q option: %d", cfgRetriesKey, clientConfig.Retries)
		}
	}

	if timeout, ok := containerDetails.Config[cfgTimeoutKey]; ok {
		var err error
		clientConfig.Timeout, err = time.ParseDuration(timeout)
		if err != nil {
			return clientConfig, fmt.Errorf("failed to parse %q option: %w", cfgTimeoutKey, err)
		}
	}

	return clientConfig, nil
}

func parseBool(value string, defaultValue bool) (bool, error) {
	if value == "" {
		return defaultValue, nil
	}

	return strconv.ParseBool(value)
}
