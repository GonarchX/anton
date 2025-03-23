package broadcaster

import (
	"github.com/rs/zerolog/log"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	broadcast "github.com/tonindexer/anton/internal/kafka/broadcast"
	"sync"
)

const (
	defaultSubChannelSize = 32
)

type BroadcastService struct {
	desc.UnimplementedExampleServiceServer
	BroadcastMessagesTopicClient *broadcast.BroadcastTopicClient
	Broadcaster                  *Broadcaster[*desc.V1GetDataStreamResponse]
}

// Broadcaster структура, агрегирующая в себе подписчиков и позволяющая рассылать им указанные сообщения.
type Broadcaster[T any] struct {
	rw *sync.RWMutex
	// subs Список подписчиков
	subs map[string]chan T
}

func NewBroadcaster[T any]() *Broadcaster[T] {
	return &Broadcaster[T]{
		rw:   &sync.RWMutex{},
		subs: make(map[string]chan T),
	}
}

// Subscribe создает и возвращает канал связанный с подписчиком.
func (b *Broadcaster[T]) Subscribe(id string) chan T {
	log.Debug().Str("sub_id", id).Msg("subscribe on stream")
	b.rw.Lock()
	defer b.rw.Unlock()
	ch := make(chan T, defaultSubChannelSize)
	b.subs[id] = ch
	return ch
}

func (b *Broadcaster[T]) Unsubscribe(id string) {
	log.Debug().Str("sub_id", id).Msg("unsubscribe from stream")
	b.rw.Lock()
	defer b.rw.Unlock()
	delete(b.subs, id)
}

func (b *Broadcaster[T]) Broadcast(value T) {
	b.rw.RLock()
	defer b.rw.RUnlock()
	for id, s := range b.subs {
		select {
		case s <- value:
		default:
			// Если не можем записать, то просто скипаем
			log.Debug().Str("sub_id", id).Msg("skip broadcast message due to subscriber channel is not empty")
			continue
		}
	}
}
