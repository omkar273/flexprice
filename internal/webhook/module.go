package webhook

import (
	"github.com/flexprice/flexprice/internal/config"
	kafkaProducerPkg "github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	"github.com/flexprice/flexprice/internal/pubsub/memory"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/handler"
	"github.com/flexprice/flexprice/internal/webhook/payload"
	"github.com/flexprice/flexprice/internal/webhook/publisher"
	"go.uber.org/fx"
)

// Module provides all webhook-related dependencies
var Module = fx.Options(
	// Core dependencies
	fx.Provide(
		providePubSub,
	),

	// Webhook components
	fx.Provide(
		provideWebhookPublisher,
		handler.NewHandler,
		providePayloadBuilderFactory,
		NewWebhookService,
	),
)

// providePayloadBuilderFactory creates a new payload builder factory with all required services
func providePayloadBuilderFactory(
	invoiceService service.InvoiceService,
	planService service.PlanService,
	priceService service.PriceService,
	entitlementService service.EntitlementService,
	featureService service.FeatureService,
	subscriptionService service.SubscriptionService,
	walletService service.WalletService,
	customerService service.CustomerService,
	paymentService service.PaymentService,
	sentry *sentry.Service,
	creditNoteService service.CreditNoteService,
) payload.PayloadBuilderFactory {
	services := payload.NewServices(
		invoiceService,
		planService,
		priceService,
		entitlementService,
		featureService,
		subscriptionService,
		walletService,
		customerService,
		paymentService,
		sentry,
		creditNoteService,
	)
	return payload.NewPayloadBuilderFactory(services)
}

func providePubSub(
	cfg *config.Configuration,
	logger *logger.Logger,
) pubsub.PubSub {
	switch cfg.Webhook.PubSub {
	case types.KafkaPubSub:
		pubSub, err := kafka.NewPubSubFromConfig(cfg, logger, cfg.Webhook.ConsumerGroup)
		if err != nil {
			logger.Fatalw("failed to create kafka pubsub for webhooks", "error", err)
		}
		return pubSub
	case types.MemoryPubSub:
		return memory.NewPubSub(cfg, logger)
	default:
		logger.Fatalw("unsupported webhook pubsub type", "type", cfg.Webhook.PubSub)
	}
	return nil
}

// provideWebhookPublisher returns a webhook publisher. When webhook.pubsub is kafka, uses the shared Kafka producer (publishing goes to Kafka); otherwise uses the in-memory PubSub.
func provideWebhookPublisher(
	cfg *config.Configuration,
	logger *logger.Logger,
	pubSub pubsub.PubSub,
	producer *kafkaProducerPkg.Producer,
) (publisher.WebhookPublisher, error) {
	if cfg.Webhook.PubSub == types.KafkaPubSub {
		return publisher.NewPublisherFromProducer(producer, cfg, logger)
	}
	return publisher.NewPublisher(pubSub, cfg, logger)
}
