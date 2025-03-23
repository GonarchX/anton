package indexer

import (
	"bytes"
	"encoding/gob"
	"github.com/stretchr/testify/require"
	kafka "github.com/tonindexer/anton/internal/kafka/unseen_block_info"

	"github.com/xssnick/tonutils-go/ton"
	"testing"
)

func serialize(data kafka.UnseenBlockInfo) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(data)
	return buf.Bytes(), err
}

func deserialize(data []byte) (kafka.UnseenBlockInfo, error) {
	var result kafka.UnseenBlockInfo
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&result)
	return result, err
}

func TestSerialization(t *testing.T) {
	original := kafka.UnseenBlockInfo{
		Master: &ton.BlockIDExt{
			Workchain: 0,
			Shard:     9223372036854775807,
			SeqNo:     100,
			RootHash:  []byte{0xAA, 0xBB, 0xCC},
			FileHash:  []byte{0x11, 0x22, 0x33},
		},
		Shards: []*ton.BlockIDExt{
			{
				Workchain: -1,
				Shard:     111,
				SeqNo:     200,
				RootHash:  []byte{0xDD, 0xEE, 0xFF},
				FileHash:  []byte{0x44, 0x55, 0x66},
			},
		},
	}

	data, err := serialize(original)
	require.NoError(t, err)

	decoded, err := deserialize(data)
	require.NoError(t, err)

	require.Equal(t, original, decoded)
}
