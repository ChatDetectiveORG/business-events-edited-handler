package application

import (
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints/deletedMessage"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints/editedMessage"

	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/config"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/rabbitmq"
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	"github.com/ChatDetectiveORG/shared/amqputil"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	errors          = make(chan *e.ErrorInfo, 1000)
	rabbitmqChannel *amqp.Channel
)

const shardCount = 64

func initRabbitmqQueue(cfg *config.Config) (<-chan amqp.Delivery, []string, *amqp.Channel, *e.ErrorInfo) {
	rabbitmqChannel, err := rabbitmq.NewRabbitmqChannel(cfg)
	if !err.IsNil() {
		return nil, nil, nil, err
	}

	// Reasonable default; per-session ordering is enforced by our handler goroutines anyway.
	_ = rabbitmqChannel.Qos(50, 0, false)

	merged := make(chan amqp.Delivery, 1000)
	consumerTags := make([]string, 0, shardCount)

	var forwardWg sync.WaitGroup
	forwardWg.Add(shardCount)

	podID := os.Getenv("POD_ID")
	if podID == "" {
		podID = "unknown"
	}

	for i := 0; i < shardCount; i++ {
		q := fmtShardQueue(i)
		tag := fmt.Sprintf("events-%s-%s", podID, q)
		consumerTags = append(consumerTags, tag)

		consumer, unwrappedError := rabbitmqChannel.Consume(
			q,
			tag,
			false, // manual acks
			false,
			false,
			false,
			amqp.Table{},
		)
		if unwrappedError != nil {
			_ = rabbitmqChannel.Close()
			return nil, nil, nil, e.FromError(unwrappedError, "failed to init consumer").WithSeverity(e.Critical).WithData(map[string]any{"queue": q})
		}

		go func(c <-chan amqp.Delivery) {
			defer forwardWg.Done()
			for d := range c {
				merged <- d
			}
		}(consumer)
	}

	go func() {
		forwardWg.Wait()
		close(merged)
	}()

	return merged, consumerTags, rabbitmqChannel, e.Nil()
}

func ListenToRabbitmq(cfg *config.Config, ctx context.Context, wg *sync.WaitGroup) *e.ErrorInfo {
	router.ReplicaCount = cfg.RuntimeConfig.NumRoutingGorutines
	if router.ReplicaCount <= 0 {
		router.ReplicaCount = shardCount
	}
	podID := cfg.RuntimeConfig.PodID
	if podID == "" {
		podID = "unknown"
	}
	router.InitSharding(podID, wg, ctx)

	go hanleError(errors, ctx, wg)

	wg.Add(1)
	go func() {
		defer wg.Done()
		amqputil.RunConsumerLoop(ctx, amqputil.ConsumerConfig{
			Dial: func() (*amqputil.ConsumerSession, error) {
				deliveries, tags, ch, dialErr := initRabbitmqQueue(cfg)
				if !dialErr.IsNil() {
					return nil, dialErr
				}
				return &amqputil.ConsumerSession{
					Deliveries: deliveries,
					Channel:    ch,
					Cleanup: func() {
						for _, tag := range tags {
							_ = ch.Cancel(tag, false)
						}
						_ = ch.Close()
					},
				}, nil
			},
			OnConnect: func(session *amqputil.ConsumerSession) {
				if connectErr := router.ConnectRabbitmqSession(session.Channel, openRabbitmqChannel(cfg), wg, podID, ctx); !connectErr.IsNil() {
					log.Printf("business-events-edited-handler: outgoing session setup failed: %s", connectErr.JSON())
				}
			},
			OnDelivery: func(delivery amqp.Delivery) {
				log.Printf("trace=%s received rk=%s", delivery.CorrelationId, delivery.RoutingKey)
				if routeErr := router.HandleUpdate(delivery); !routeErr.IsNil() {
					errors <- routeErr.WithData(map[string]any{"rk": delivery.RoutingKey}).WithSeverity(e.Critical)
					if nackErr := delivery.Nack(false, false); nackErr != nil {
						errors <- e.FromError(nackErr, "failed to nack delivery").WithSeverity(e.Critical)
					}
					return
				}
				if ackErr := delivery.Ack(false); ackErr != nil {
					errors <- e.FromError(ackErr, "Ошибка подтверждения получения")
				}
			},
		})
	}()

	return e.Nil()
}

func hanleError(src chan (*e.ErrorInfo), context context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	for {
		select {
		case <-context.Done():
			return
		case err := <-src:
			log.Println(err.JSON())
		}
	}
}

func fmtShardQueue(i int) string {
	return fmt.Sprintf("%s.q%02d", config.PodType, i)
}

func openRabbitmqChannel(cfg *config.Config) func() (*amqp.Channel, error) {
	return func() (*amqp.Channel, error) {
		ch, err := rabbitmq.NewRabbitmqChannel(cfg)
		if !err.IsNil() {
			return nil, err
		}
		return ch, nil
	}
}

var router h.Router = h.Router{
	ErrorChannel:    errors,
	RabbitmqChannel: rabbitmqChannel,
	Endpoints: []h.Endpoint{
		deletedMessage.NewDeletedMessageEndpoint(),
		editedMessage.NewEditedMessageEndpoint(),
	},
}
