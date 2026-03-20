package kafka

import (
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/flexprice/flexprice/internal/config"
)

type Producer struct {
	*kafka.Publisher
}

func NewProducer(cfg *config.Configuration) (*Producer, error) {
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
		watermill.NewStdLogger(false, false),
	)
	if err != nil {
		return nil, err
	}

	return &Producer{Publisher: publisher}, nil
}
