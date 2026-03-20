package kafka

import (
	"github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
)

type Producer struct {
	*kafka.Publisher
}

func NewProducer(cfg *config.Configuration, log *logger.Logger) (*Producer, error) {
	saramaConfig := GetSaramaConfig(cfg)
	if saramaConfig != nil {
		// add producer configs
		saramaConfig.Producer.Return.Successes = true
		saramaConfig.Producer.Return.Errors = true
	}

	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:               cfg.Kafka.Brokers,
			Marshaler:             kafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
		},
		log.GetWatermillLogger(),
	)
	if err != nil {
		return nil, err
	}

	return &Producer{Publisher: publisher}, nil
}
