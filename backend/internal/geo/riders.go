package geo

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	ridersOnlineKey  = "riders:online"
	riderSeenTTL     = 15 * time.Second
	riderClaimTTL    = 30 * time.Second
	riderLocationTTL = 2 * time.Second
)

var releaseClaimScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

type Riders struct {
	rdb *redis.Client
}

func NewRiders(rdb *redis.Client) *Riders {
	return &Riders{rdb: rdb}
}

func (r *Riders) Ping(ctx context.Context, riderID int64, lat, lng float64) error {
	id := strconv.FormatInt(riderID, 10)
	pipe := r.rdb.Pipeline()

	pipe.GeoAdd(ctx, ridersOnlineKey, &redis.GeoLocation{
		Name:      id,
		Longitude: lng,
		Latitude:  lat,
	})
	pipe.Set(ctx, "rider:seen:"+id, "1", riderSeenTTL)

	_, err := pipe.Exec(ctx)
	return err
}

func (r *Riders) Offline(ctx context.Context, riderID int64) error {
	id := strconv.FormatInt(riderID, 10)

	pipe := r.rdb.Pipeline()
	pipe.ZRem(ctx, ridersOnlineKey, id)
	pipe.Del(ctx, "rider:seen:"+id)

	_, err := pipe.Exec(ctx)
	return err
}

func (r *Riders) Nearest(ctx context.Context, lat, lng, km float64, count int) ([]int64, error) {
	if count <= 0 {
		return []int64{}, nil
	}

	members, err := r.rdb.GeoSearch(ctx, ridersOnlineKey, &redis.GeoSearchQuery{
		Longitude:  lng,
		Latitude:   lat,
		Radius:     km,
		RadiusUnit: "km",
		Sort:       "ASC",
		Count:      count,
	}).Result()
	if err != nil {
		return nil, err
	}

	if len(members) == 0 {
		return []int64{}, nil
	}

	pipe := r.rdb.Pipeline()
	seenCommands := make([]*redis.StringCmd, len(members))

	for i, member := range members {
		seenCommands[i] = pipe.Get(ctx, "rider:seen:"+member)
	}

	_, execErr := pipe.Exec(ctx)
	if execErr != nil && execErr != redis.Nil {
		return nil, execErr
	}

	result := make([]int64, 0, len(members))
	stale := make([]interface{}, 0)

	for i, member := range members {
		_, seenErr := seenCommands[i].Result()
		if seenErr == redis.Nil {
			stale = append(stale, member)
			continue
		}
		if seenErr != nil {
			return nil, seenErr
		}

		id, parseErr := strconv.ParseInt(member, 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}

		result = append(result, id)
	}

	if len(stale) > 0 {
		if err := r.rdb.ZRem(ctx, ridersOnlineKey, stale...).Err(); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (r *Riders) Claim(ctx context.Context, orderID, riderID int64) (bool, error) {
	key := "order:" + strconv.FormatInt(orderID, 10) + ":claim"
	value := strconv.FormatInt(riderID, 10)

	return r.rdb.SetNX(ctx, key, value, riderClaimTTL).Result()
}

func (r *Riders) ClaimedBy(ctx context.Context, orderID int64) (int64, error) {
	key := "order:" + strconv.FormatInt(orderID, 10) + ":claim"

	value, err := r.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	riderID, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}

	return riderID, nil
}

func (r *Riders) Release(ctx context.Context, orderID, riderID int64) (bool, error) {
	key := "order:" + strconv.FormatInt(orderID, 10) + ":claim"
	value := strconv.FormatInt(riderID, 10)

	deleted, err := releaseClaimScript.Run(ctx, r.rdb, []string{key}, value).Int()
	if err != nil {
		return false, err
	}

	return deleted == 1, nil
}

func (r *Riders) AllowLocation(ctx context.Context, riderID int64) (bool, error) {
	key := "rider:loc:" + strconv.FormatInt(riderID, 10)

	return r.rdb.SetNX(ctx, key, "1", riderLocationTTL).Result()
}

func (r *Riders) PublishLocation(ctx context.Context, orderID int64, lat, lng float64) error {
	payload, err := json.Marshal(struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}{
		Lat: lat,
		Lng: lng,
	})
	if err != nil {
		return err
	}

	channel := "order:" + strconv.FormatInt(orderID, 10) + ":loc"
	return r.rdb.Publish(ctx, channel, payload).Err()
}
