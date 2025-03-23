package core

import (
	"github.com/stretchr/testify/require"
	"github.com/tonindexer/anton/addr"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	"testing"
)

func TestAccountState_ToProto(t *testing.T) {
	addressToSlice := func(address [33]byte) []byte {
		return address[:]
	}

	tests := []struct {
		name         string
		accountState AccountState
		want         *desc.AccountState
		wantErr      bool
	}{
		{
			name: "happy case - valid mapping",
			accountState: AccountState{
				Address: addr.Address([33]byte{1, 2, 3}),
				Label: &AddressLabel{
					Address:    addr.Address([33]byte{5, 6, 7}),
					Name:       "Hello world",
					Categories: []LabelCategory{CentralizedExchange, Scam, Scam},
				},
				Workchain:  -1,
				Shard:      123456,
				BlockSeqNo: 76543,
				IsActive:   true,
			},
			want: &desc.AccountState{
				Address: &desc.Address{Value: addressToSlice([33]byte{1, 2, 3})},
				Label: &desc.AddressLabel{
					Address:    &desc.Address{Value: addressToSlice([33]byte{5, 6, 7})},
					Name:       "Hello world",
					Categories: []desc.LabelCategory{desc.LabelCategory_CENTRALIZED_EXCHANGE, desc.LabelCategory_SCAM, desc.LabelCategory_SCAM},
				},
				Workchain:  -1,
				Shard:      123456,
				BlockSeqNo: 76543,
				IsActive:   true,
			},
			wantErr: false,
		},
		{
			name: "invalid label category - should return error",
			accountState: AccountState{
				Address: addr.Address([33]byte{1, 2, 3}),
				Label: &AddressLabel{
					Address:    addr.Address([33]byte{5, 6, 7}),
					Name:       "Hello world",
					Categories: []LabelCategory{LabelCategory("invalid value")},
				},
				Workchain:  -1,
				Shard:      123456,
				BlockSeqNo: 76543,
				IsActive:   true,
			},
			want: &desc.AccountState{
				Address: &desc.Address{Value: addressToSlice([33]byte{1, 2, 3})},
				Label: &desc.AddressLabel{
					Address:    &desc.Address{Value: addressToSlice([33]byte{5, 6, 7})},
					Name:       "Hello world",
					Categories: []desc.LabelCategory{desc.LabelCategory_CENTRALIZED_EXCHANGE, desc.LabelCategory_SCAM, desc.LabelCategory_SCAM},
				},
				Workchain:  -1,
				Shard:      123456,
				BlockSeqNo: 76543,
				IsActive:   true,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := tt.accountState
			got, err := a.ToProto()

			if (err != nil) != tt.wantErr {
				t.Errorf("ToProto() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				require.Equal(t, tt.want, got)
			}
		})
	}
}
