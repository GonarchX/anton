package broadcaster

import (
	"context"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *BroadcastService) V1GetDataStream(req *desc.V1GetDataStreamRequest, stream desc.ExampleService_V1GetDataStreamServer) error {
	subId := req.GetId()
	if subId == "" {
		return status.Errorf(codes.InvalidArgument, "subscription ID is required")
	}

	subCh := s.Broadcaster.Subscribe(subId)
	defer s.Broadcaster.Unsubscribe(subId)

	for ch := range subCh {
		err := stream.Send(ch)
		if err != nil {
			return status.Errorf(codes.Unavailable, "failed to send data to stream: %v", err)
		}
	}

	return nil
}

// StartBroadcasting начинает слушать Kafka топик с обновлениями по блокчейну,
// которые затем отправляются подписчикам через Broadcaster.
func (s *BroadcastService) StartBroadcasting(ctx context.Context) {
	dataCh := s.BroadcastMessagesTopicClient.Consume(ctx)
	go func() {
		for {
			select {
			case v := <-dataCh:
				s.Broadcaster.Broadcast(v)
			case <-ctx.Done():
				return
			}
		}
	}()
}
