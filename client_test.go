package main

import (
	"os"
	"testing"

	"go.uber.org/zap"
)

func TestSendMessage(t *testing.T) {
	client, err := NewClient(zap.NewNop(), ClientConfig{
		Endpoint:  os.Getenv("CLS_ENDPOINT"),
		SecretID:  os.Getenv("CLS_SECRET_ID"),
		SecretKey: os.Getenv("CLS_SECRET_KEY"),
		TopicID:   os.Getenv("CLS_TOPIC_ID"),
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = client.SendMessage(`{"a": "b"}`)
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("failed to close client: %v", err)
	}
}
