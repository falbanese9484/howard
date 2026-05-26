package types

import (
	"sync"
	"time"
)

type Model struct {
	Name       string   `json:"name"`
	Modalities []string `json:"modalities"`
}

type ModelCache struct {
	Expiry      time.Duration
	Models      []Model
	Mutex       sync.RWMutex
	LastUpdated time.Time
}

func NewModelCache(refreshSeconds int64) *ModelCache {
	return &ModelCache{
		Expiry: time.Duration(refreshSeconds) * time.Second,
		Models: []Model{},
	}
}

func (mc *ModelCache) Store(models []Model) {
	mc.Mutex.Lock()
	defer mc.Mutex.Unlock()
	mc.Models = models
	mc.LastUpdated = time.Now()
}

func (mc *ModelCache) Load() []Model {
	mc.Mutex.RLock()
	defer mc.Mutex.RUnlock()
	result := make([]Model, len(mc.Models))
	copy(result, mc.Models)
	return result
}

func (mc *ModelCache) IsStale() bool {
	mc.Mutex.RLock()
	defer mc.Mutex.RUnlock()
	return time.Since(mc.LastUpdated) > mc.Expiry
}
