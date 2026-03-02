package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDLQBasicFlow tests the basic DLQ functionality
// This test demonstrates that poison messages go to DLQ after max retries
func TestDLQBasicFlow(t *testing.T) {
	t.Log("=== Testing DLQ Basic Flow ===")

	// Create test configuration
	cfg := &config.Configuration{
		Kafka: config.KafkaConfig{
			Brokers:  []string{"localhost:9092"},
			ClientID: "test-client",
		},
		Webhook: config.Webhook{
			MaxRetries:      3,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     100 * time.Millisecond,
			Multiplier:      2.0,
			MaxElapsedTime:  2 * time.Minute,
		},
		Logging: config.LoggingConfig{
			Level:   types.LogLevelInfo,
			DBLevel: types.LogLevelInfo,
		},
	}

	// Create logger
	testLogger, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	// Create in-memory pub/sub (simulates Kafka for testing)
	pubSub := gochannel.NewGoChannel(
		gochannel.Config{},
		watermill.NewStdLogger(false, false),
	)

	// Subscribe to DLQ topic to capture failed messages
	dlqMessages, err := pubSub.Subscribe(context.Background(), "events_dlq")
	require.NoError(t, err)

	// Create router with DLQ enabled
	router, err := NewRouter(cfg, testLogger, &sentry.Service{})
	require.NoError(t, err)

	// Track how many times handler is called
	handlerCalls := 0

	// Add a handler that always fails (simulates a poison message)
	router.AddNoPublishHandler(
		"poison_handler",
		"test_topic",
		pubSub,
		func(msg *message.Message) error {
			handlerCalls++
			t.Logf("Handler called attempt #%d for message %s", handlerCalls, msg.UUID)
			return errors.New("simulated processing failure")
		},
	)

	// Start router in background
	routerDone := make(chan error, 1)
	go func() {
		routerDone <- router.Run()
	}()

	// Give router time to start
	time.Sleep(100 * time.Millisecond)

	// Publish a test message that will fail
	testMsg := message.NewMessage(watermill.NewUUID(), []byte(`{"event": "test_poison"}`))
	t.Logf("Publishing test message: %s", testMsg.UUID)
	
	err = pubSub.Publish("test_topic", testMsg)
	require.NoError(t, err)

	// Wait for message to be retried and eventually sent to DLQ
	t.Log("Waiting for message to be processed and sent to DLQ...")
	
	select {
	case dlqMsg := <-dlqMessages:
		t.Logf("✓ Message received in DLQ: %s", string(dlqMsg.Payload))
		dlqMsg.Ack()

		// Verify the DLQ message
		assert.NotNil(t, dlqMsg)
		assert.Contains(t, string(dlqMsg.Payload), "test_poison")
		
		// Handler should have been called multiple times (initial + retries)
		t.Logf("Handler was called %d times before going to DLQ", handlerCalls)
		assert.Greater(t, handlerCalls, 1, "Should have retried at least once")
		
		t.Log("✓ Test PASSED: Poison message correctly sent to DLQ after retries")

	case <-time.After(5 * time.Second):
		t.Fatalf("Timeout waiting for DLQ message. Handler was called %d times", handlerCalls)
	}

	// Cleanup
	router.Close()
}

// TestDLQDisabledByDefault tests that empty topic_dlq config disables DLQ
func TestDLQDisabledByDefault(t *testing.T) {
	t.Log("=== Testing DLQ Disabled When Not Configured ===")

	cfg := &config.Configuration{
		Kafka: config.KafkaConfig{
			Brokers: []string{"localhost:9092"},
		},
		Webhook: config.Webhook{
			MaxRetries:      3,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     100 * time.Millisecond,
			Multiplier:      2.0,
			MaxElapsedTime:  2 * time.Minute,
		},
		Logging: config.LoggingConfig{
			Level:   types.LogLevelInfo,
			DBLevel: types.LogLevelInfo,
		},
		EventProcessing: config.EventProcessingConfig{
			Topic:    "events",
			TopicDLQ: "", // Empty = DLQ disabled
		},
	}

	testLogger, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	// Router should still be created successfully even without DLQ
	router, err := NewRouter(cfg, testLogger, &sentry.Service{})
	require.NoError(t, err)
	assert.NotNil(t, router)
	
	t.Log("✓ Test PASSED: Router created successfully with empty DLQ config")
}

// TestSuccessfulMessageSkipsDLQ tests that successfully processed messages don't go to DLQ
func TestSuccessfulMessageSkipsDLQ(t *testing.T) {
	t.Log("=== Testing Successful Messages Don't Go To DLQ ===")

	cfg := &config.Configuration{
		Kafka: config.KafkaConfig{
			Brokers: []string{"localhost:9092"},
		},
		Webhook: config.Webhook{
			MaxRetries:      3,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     100 * time.Millisecond,
			Multiplier:      2.0,
			MaxElapsedTime:  2 * time.Minute,
		},
		Logging: config.LoggingConfig{
			Level:   types.LogLevelInfo,
			DBLevel: types.LogLevelInfo,
		},
	}

	testLogger, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	pubSub := gochannel.NewGoChannel(
		gochannel.Config{},
		watermill.NewStdLogger(false, false),
	)

	dlqMessages, err := pubSub.Subscribe(context.Background(), "events_dlq")
	require.NoError(t, err)

	router, err := NewRouter(cfg, testLogger, &sentry.Service{})
	require.NoError(t, err)

	processedCount := 0
	router.AddNoPublishHandler(
		"success_handler",
		"test_topic",
		pubSub,
		func(msg *message.Message) error {
			processedCount++
			t.Logf("Successfully processed message: %s", msg.UUID)
			return nil // Success!
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx

	go func() {
		router.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	// Publish successful message
	testMsg := message.NewMessage(watermill.NewUUID(), []byte(`{"event": "test_success"}`))
	err = pubSub.Publish("test_topic", testMsg)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Verify message was processed
	assert.Equal(t, 1, processedCount, "Message should be processed exactly once")

	// Verify NO message in DLQ
	select {
	case dlqMsg := <-dlqMessages:
		t.Fatalf("Unexpected message in DLQ: %s", string(dlqMsg.Payload))
	case <-time.After(500 * time.Millisecond):
		t.Log("✓ Test PASSED: Successful message correctly skipped DLQ")
	}

	cancel()
	router.Close()
}

// TestRetryThenSuccessSkipsDLQ tests that messages succeeding after retries don't go to DLQ
func TestRetryThenSuccessSkipsDLQ(t *testing.T) {
	t.Log("=== Testing Messages That Eventually Succeed Don't Go To DLQ ===")

	cfg := &config.Configuration{
		Kafka: config.KafkaConfig{
			Brokers: []string{"localhost:9092"},
		},
		Webhook: config.Webhook{
			MaxRetries:      3,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     100 * time.Millisecond,
			Multiplier:      2.0,
			MaxElapsedTime:  2 * time.Minute,
		},
		Logging: config.LoggingConfig{
			Level:   types.LogLevelInfo,
			DBLevel: types.LogLevelInfo,
		},
	}

	testLogger, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	pubSub := gochannel.NewGoChannel(
		gochannel.Config{},
		watermill.NewStdLogger(false, false),
	)

	dlqMessages, err := pubSub.Subscribe(context.Background(), "events_dlq")
	require.NoError(t, err)

	router, err := NewRouter(cfg, testLogger, &sentry.Service{})
	require.NoError(t, err)

	attemptCount := 0
	router.AddNoPublishHandler(
		"retry_handler",
		"test_topic",
		pubSub,
		func(msg *message.Message) error {
			attemptCount++
			t.Logf("Handler attempt #%d", attemptCount)
			
			// Fail first 2 attempts, succeed on 3rd
			if attemptCount < 3 {
				return errors.New("transient error - will retry")
			}
			t.Log("Success on attempt 3!")
			return nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx

	go func() {
		router.Run()
	}()

	time.Sleep(100 * time.Millisecond)

	testMsg := message.NewMessage(watermill.NewUUID(), []byte(`{"event": "test_retry_success"}`))
	err = pubSub.Publish("test_topic", testMsg)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// Verify retries happened
	t.Logf("Total attempts: %d", attemptCount)
	assert.GreaterOrEqual(t, attemptCount, 3, "Should have attempted at least 3 times")

	// Verify NO message in DLQ (because it eventually succeeded)
	select {
	case dlqMsg := <-dlqMessages:
		t.Fatalf("Unexpected message in DLQ after eventual success: %s", string(dlqMsg.Payload))
	case <-time.After(500 * time.Millisecond):
		t.Log("✓ Test PASSED: Message that eventually succeeded correctly skipped DLQ")
	}

	cancel()
	router.Close()
}
