package realtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/yashdagar/zomato-prep/backend/internal/ws"
)

func Publish(ctx context.Context, rdb *redis.Client, userID int64, payload []byte) error {
	return rdb.Publish(ctx, fmt.Sprintf("user:%d", userID), payload).Err()
}

func Subscribe(ctx context.Context, rdb *redis.Client, hub *ws.Hub) error {
	pubsub := rdb.PSubscribe(ctx, "user:*")
	defer pubsub.Close()

	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return nil
			}

			channel := msg.Channel
			if !strings.HasPrefix(channel, "user:") {
				continue
			}

			userID, err := strconv.ParseInt(strings.TrimPrefix(channel, "user:"), 10, 64)
			if err != nil {
				continue
			}

			hub.Send(userID, []byte(msg.Payload))
		}
	}
}
