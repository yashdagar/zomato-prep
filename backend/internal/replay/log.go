package replay

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Entry struct {
	ID      int64
	Payload []byte
}

type Log struct {
	rdb    *redis.Client
	maxLen int64
	ttl    time.Duration
}

func NewLog(rdb *redis.Client, maxLen int64, ttl time.Duration) *Log {
	return &Log{
		rdb:    rdb,
		maxLen: maxLen,
		ttl:    ttl,
	}
}

func (l *Log) sequenceKey(userID int64) string {
	return "replay:user:" + strconv.FormatInt(userID, 10) + ":sequence"
}

func (l *Log) entriesKey(userID int64) string {
	return "replay:user:" + strconv.FormatInt(userID, 10) + ":entries"
}

func (l *Log) Append(ctx context.Context, userID int64, payload []byte) (int64, error) {
	sequenceKey := l.sequenceKey(userID)
	entriesKey := l.entriesKey(userID)

	id, err := l.rdb.Incr(ctx, sequenceKey).Result()
	if err != nil {
		return 0, err
	}

	member := strconv.FormatInt(id, 10) + ":" + string(payload)

	pipe := l.rdb.TxPipeline()

	pipe.ZAdd(ctx, entriesKey, redis.Z{
		Score:  float64(id),
		Member: member,
	})

	pipe.ZRemRangeByRank(ctx, entriesKey, 0, -l.maxLen-1)

	pipe.Expire(ctx, sequenceKey, l.ttl)
	pipe.Expire(ctx, entriesKey, l.ttl)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (l *Log) Since(ctx context.Context, userID int64, afterID int64) ([]Entry, error) {
	entriesKey := l.entriesKey(userID)

	members, err := l.rdb.ZRangeByScore(ctx, entriesKey, &redis.ZRangeBy{
		Min: "(" + strconv.FormatInt(afterID, 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(members))

	for _, member := range members {
		idText, payload, ok := strings.Cut(member, ":")
		if !ok {
			return nil, strconv.ErrSyntax
		}

		id, err := strconv.ParseInt(idText, 10, 64)
		if err != nil {
			return nil, err
		}

		entries = append(entries, Entry{
			ID:      id,
			Payload: []byte(payload),
		})
	}

	return entries, nil
}
