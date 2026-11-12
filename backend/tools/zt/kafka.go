package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func kafkaBrokers() string {
	return envOr("KAFKA_BROKERS", "127.0.0.1:9092")
}

// resetKafka deletes the order event topics so a run never sees events left
// by an earlier run, whose orders no longer exist in the fresh database.
func resetKafka() error {
	cl, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafkaBrokers(), ",")...))
	if err != nil {
		return err
	}
	defer cl.Close()
	adm := kadm.NewClient(cl)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := adm.ListBrokers(ctx); err != nil {
		return fmt.Errorf("Kafka is not reachable at %s; start Redpanda with make up", kafkaBrokers())
	}
	if _, err := adm.DeleteTopics(ctx, resetTopics...); err != nil {
		return fmt.Errorf("resetting Kafka topics: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		res, err := adm.CreateTopics(ctx, 3, 1, nil, resetTopics...)
		if err == nil && res.Error() == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("recreating Kafka topics %s timed out", strings.Join(resetTopics, ", "))
}
