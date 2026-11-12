package e2e_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func kafka(t *testing.T) []string {
	t.Helper()
	if e := os.Getenv("TEST_KAFKA_ERROR"); e != "" {
		t.Fatal(e)
	}
	return strings.Split(needDB(t, "TEST_KAFKA_BROKERS"), ",")
}

func producer(t *testing.T) *kgo.Client {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(kafka(t)...), kgo.AllowAutoTopicCreation())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	return cl
}

func produce(t *testing.T, topic, key, value string, headers ...string) {
	t.Helper()
	rec := &kgo.Record{Topic: topic, Key: []byte(key), Value: []byte(value)}
	for i := 0; i+1 < len(headers); i += 2 {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: headers[i], Value: []byte(headers[i+1])})
	}
	if err := producer(t).ProduceSync(t.Context(), rec).FirstErr(); err != nil {
		t.Fatalf("produce to %s: %v", topic, err)
	}
}

// records reads a topic from the start until done says it has what it needs
// or the timeout passes, and returns everything it read.
func records(t *testing.T, topic string, timeout time.Duration, done func([]*kgo.Record) bool) []*kgo.Record {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(kafka(t)...), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()), kgo.AllowAutoTopicCreation())
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	var got []*kgo.Record
	for ctx.Err() == nil {
		if done != nil && done(got) {
			return got
		}
		poll, cancelPoll := context.WithTimeout(ctx, 500*time.Millisecond)
		cl.PollFetches(poll).EachRecord(func(r *kgo.Record) { got = append(got, r) })
		cancelPoll()
	}
	return got
}

func header(r *kgo.Record, key string) string {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
