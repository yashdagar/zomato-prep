package topics

import (
	"sort"
	"sync"
)

type Registry struct {
	mu     sync.Mutex
	topics map[string]map[int64]struct{}
}

func New() *Registry {
	return &Registry{
		topics: make(map[string]map[int64]struct{}),
	}
}

func (r *Registry) Subscribe(topic string, userID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.topics[topic] == nil {
		r.topics[topic] = make(map[int64]struct{})
	}

	r.topics[topic][userID] = struct{}{}
}

func (r *Registry) Unsubscribe(topic string, userID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	subscribers, ok := r.topics[topic]
	if !ok {
		return
	}

	delete(subscribers, userID)

	if len(subscribers) == 0 {
		delete(r.topics, topic)
	}
}

func (r *Registry) RemoveUser(userID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for topic, subscribers := range r.topics {
		delete(subscribers, userID)

		if len(subscribers) == 0 {
			delete(r.topics, topic)
		}
	}
}

func (r *Registry) Subscribers(topic string) []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	subscribers := r.topics[topic]
	ids := make([]int64, 0, len(subscribers))

	for userID := range subscribers {
		ids = append(ids, userID)
	}

	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})

	return ids
}

func (r *Registry) Publish(topic string, msg []byte, send func(userID int64, msg []byte) bool) int {
	r.mu.Lock()

	subscribers := r.topics[topic]
	ids := make([]int64, 0, len(subscribers))

	for userID := range subscribers {
		ids = append(ids, userID)
	}

	r.mu.Unlock()

	accepted := 0

	for _, userID := range ids {
		if send(userID, msg) {
			accepted++
		}
	}

	return accepted
}
