// PushStore persists Web Push subscription objects in the shared durable store.
package web

import (
	"encoding/json"
	"fmt"
	"log"

	"inbrix/storage"
)

const pushBucket = "push_subscriptions"

type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func subscriptionKey(endpoint string) string {
	if len(endpoint) > 512 {
		return endpoint[:512]
	}
	return endpoint
}

type PushStore struct{ kv storage.KV }

func NewPushStore(kv storage.KV) *PushStore { return &PushStore{kv: kv} }

func (s *PushStore) namespace(username string) string { return pushBucket + ":" + username }

func (s *PushStore) Save(username string, sub PushSubscription) error {
	raw, err := json.Marshal(sub)
	if err != nil {
		return fmt.Errorf("pushstore: marshal: %w", err)
	}
	return s.kv.Set(s.namespace(username), subscriptionKey(sub.Endpoint), raw)
}

func (s *PushStore) Delete(username, endpoint string) error {
	return s.kv.Delete(s.namespace(username), subscriptionKey(endpoint))
}

func (s *PushStore) All(username string) ([]PushSubscription, error) {
	values, err := s.kv.List(s.namespace(username), "")
	if err != nil {
		return nil, err
	}
	subs := make([]PushSubscription, 0, len(values))
	for _, value := range values {
		var sub PushSubscription
		if err := json.Unmarshal(value, &sub); err != nil {
			log.Printf("pushstore: unmarshal subscription: %v", err)
			continue
		}
		subs = append(subs, sub)
	}
	return subs, nil
}
