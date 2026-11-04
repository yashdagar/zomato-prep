//go:build w5o2

package topics_test

import (
	"reflect"
	"sync"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/ws/topics"
)

type sink struct {
	mu   sync.Mutex
	got  map[int64][]string
	full map[int64]bool
}

func newSink() *sink { return &sink{got: map[int64][]string{}, full: map[int64]bool{}} }

func (s *sink) send(user int64, msg []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.full[user] {
		return false
	}
	s.got[user] = append(s.got[user], string(msg))
	return true
}

func TestW5O2_PublishReachesEverySubscriberOfTheTopic(t *testing.T) {
	r := topics.New()
	r.Subscribe("order:1", 10)
	r.Subscribe("order:1", 11)
	r.Subscribe("order:2", 12)
	s := newSink()
	n := r.Publish("order:1", []byte("hi"), s.send)
	if n != 2 || !reflect.DeepEqual(s.got, map[int64][]string{10: {"hi"}, 11: {"hi"}}) {
		t.Fatalf("delivered %d, got %v", n, s.got)
	}
}

func TestW5O2_PublishToAnEmptyTopicDeliversNothing(t *testing.T) {
	r := topics.New()
	if n := r.Publish("order:9", []byte("hi"), newSink().send); n != 0 {
		t.Fatalf("delivered %d, want 0", n)
	}
}

func TestW5O2_SubscribingTwiceDeliversOnce(t *testing.T) {
	r := topics.New()
	r.Subscribe("order:1", 10)
	r.Subscribe("order:1", 10)
	s := newSink()
	if n := r.Publish("order:1", []byte("hi"), s.send); n != 1 || len(s.got[10]) != 1 {
		t.Fatalf("delivered %d, got %v", n, s.got)
	}
}

func TestW5O2_UnsubscribeStopsDelivery(t *testing.T) {
	r := topics.New()
	r.Subscribe("order:1", 10)
	r.Subscribe("order:1", 11)
	r.Unsubscribe("order:1", 10)
	s := newSink()
	r.Publish("order:1", []byte("hi"), s.send)
	if _, ok := s.got[10]; ok || len(s.got[11]) != 1 {
		t.Fatalf("got %v", s.got)
	}
}

func TestW5O2_UnsubscribingSomeoneNotSubscribedIsHarmless(t *testing.T) {
	r := topics.New()
	r.Unsubscribe("order:1", 10)
	r.Subscribe("order:1", 11)
	r.Unsubscribe("order:1", 10)
	if got := r.Subscribers("order:1"); !reflect.DeepEqual(got, []int64{11}) {
		t.Fatalf("Subscribers = %v", got)
	}
}

func TestW5O2_RemoveUserLeavesEveryTopic(t *testing.T) {
	r := topics.New()
	r.Subscribe("order:1", 10)
	r.Subscribe("order:2", 10)
	r.Subscribe("order:2", 11)
	r.RemoveUser(10)
	if len(r.Subscribers("order:1")) != 0 || !reflect.DeepEqual(r.Subscribers("order:2"), []int64{11}) {
		t.Fatalf("topics after RemoveUser: %v %v", r.Subscribers("order:1"), r.Subscribers("order:2"))
	}
}

func TestW5O2_SubscribersAreSortedAndACopy(t *testing.T) {
	r := topics.New()
	for _, u := range []int64{30, 10, 20} {
		r.Subscribe("order:1", u)
	}
	got := r.Subscribers("order:1")
	if !reflect.DeepEqual(got, []int64{10, 20, 30}) {
		t.Fatalf("Subscribers = %v", got)
	}
	got[0] = 99
	if again := r.Subscribers("order:1"); again[0] != 10 {
		t.Fatalf("changing the returned slice changed the registry: %v", again)
	}
}

func TestW5O2_PublishCountsOnlyAcceptedSends(t *testing.T) {
	r := topics.New()
	r.Subscribe("order:1", 10)
	r.Subscribe("order:1", 11)
	s := newSink()
	s.full[11] = true
	if n := r.Publish("order:1", []byte("hi"), s.send); n != 1 {
		t.Fatalf("delivered %d, want 1 because user 11 refused the message", n)
	}
}

func TestW5O2_ConcurrentUseIsRaceFree(t *testing.T) {
	r := topics.New()
	s := newSink()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int64) {
			defer wg.Done()
			for i := int64(0); i < 200; i++ {
				r.Subscribe("order:1", g*1000+i)
				r.Publish("order:1", []byte("x"), s.send)
				r.Subscribers("order:1")
				if i%3 == 0 {
					r.Unsubscribe("order:1", g*1000+i)
				}
				if i%50 == 0 {
					r.RemoveUser(g*1000 + i)
				}
			}
		}(int64(g))
	}
	wg.Wait()
}
