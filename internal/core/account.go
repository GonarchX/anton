package core

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	"github.com/tonindexer/anton/abi"
	"github.com/tonindexer/anton/addr"
	desc "github.com/tonindexer/anton/internal/generated/proto/anton/api"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/extra/bunbig"
	"github.com/uptrace/go-clickhouse/ch"
	"github.com/xssnick/tonutils-go/tlb"
)

type AccountStatus string

const (
	Uninit   = AccountStatus(tlb.AccountStatusUninit)
	Active   = AccountStatus(tlb.AccountStatusActive)
	Frozen   = AccountStatus(tlb.AccountStatusFrozen)
	NonExist = AccountStatus(tlb.AccountStatusNonExist)
)

type LabelCategory string

var (
	CentralizedExchange LabelCategory = "centralized_exchange"
	Scam                LabelCategory = "scam"
)

type AddressLabel struct {
	ch.CHModel    `ch:"address_labels" json:"-"`
	bun.BaseModel `bun:"table:address_labels" json:"-"`

	Address    addr.Address    `ch:"type:String,pk" bun:"type:bytea,pk,notnull" json:"address"`
	Name       string          `bun:"type:text" json:"name"`
	Categories []LabelCategory `ch:",lc" bun:"type:label_category[]" json:"categories,omitempty"`
}

type NFTContentData struct {
	ContentURI         string `ch:"type:String" bun:",nullzero" json:"content_uri,omitempty"`
	ContentName        string `ch:"type:String" bun:",nullzero" json:"content_name,omitempty"`
	ContentDescription string `ch:"type:String" bun:",nullzero" json:"content_description,omitempty"`
	ContentImage       string `ch:"type:String" bun:",nullzero" json:"content_image,omitempty"`
	ContentImageData   []byte `ch:"type:String" bun:",nullzero" json:"content_image_data,omitempty"`
}

type FTWalletData struct {
	JettonBalance *bunbig.Int `ch:"type:UInt256" bun:"type:numeric" json:"jetton_balance,omitempty" swaggertype:"string"`
}

type AccountStateID struct {
	Address  addr.Address `ch:"type:String"`
	LastTxLT uint64
}

type AccountState struct {
	ch.CHModel    `ch:"account_states,partition:toYYYYMM(updated_at)" json:"-"`
	bun.BaseModel `bun:"table:account_states" json:"-"`

	Address addr.Address  `ch:"type:String,pk" bun:"type:bytea,pk,notnull" json:"address"`
	Label   *AddressLabel `ch:"-" bun:"rel:has-one,join:address=address" json:"label,omitempty"`

	Workchain  int32  `bun:"type:integer,notnull" json:"workchain"`
	Shard      int64  `bun:"type:bigint,notnull" json:"shard"`
	BlockSeqNo uint32 `bun:"type:integer,notnull" json:"block_seq_no"`

	IsActive bool          `json:"is_active"`
	Status   AccountStatus `ch:",lc" bun:"type:account_status" json:"status"` // TODO: ch enum

	Balance *bunbig.Int `ch:"type:UInt256" bun:"type:numeric" json:"balance"`

	LastTxLT   uint64 `ch:",pk" bun:"type:bigint,pk,notnull" json:"last_tx_lt"`
	LastTxHash []byte `bun:"type:bytea,unique,notnull" json:"last_tx_hash"`

	StateHash []byte `bun:"type:bytea" json:"state_hash,omitempty"` // only if account is frozen
	Code      []byte `ch:"-" bun:"type:bytea" json:"code,omitempty"`
	CodeHash  []byte `bun:"type:bytea" json:"code_hash,omitempty"`
	Data      []byte `ch:"-" bun:"type:bytea" json:"data,omitempty"`
	DataHash  []byte `bun:"type:bytea" json:"data_hash,omitempty"`
	Libraries []byte `bun:"type:bytea" json:"libraries,omitempty"`

	GetMethodHashes []int32 `ch:"type:Array(UInt32)" bun:"type:integer[]" json:"get_method_hashes,omitempty"`

	Types []abi.ContractName `ch:"type:Array(String)" bun:"type:text[],array" json:"types,omitempty"`

	// common fields for FT and NFT
	OwnerAddress  *addr.Address `ch:"type:String" bun:"type:bytea" json:"owner_address,omitempty"` // universal column for many contracts
	MinterAddress *addr.Address `ch:"type:String" bun:"type:bytea" json:"minter_address,omitempty"`

	Fake bool `ch:"type:Bool" bun:"type:boolean" json:"fake"`

	ExecutedGetMethods map[abi.ContractName][]abi.GetMethodExecution `ch:"type:String" bun:"type:jsonb" json:"executed_get_methods,omitempty"`

	// TODO: remove this
	NFTContentData
	FTWalletData

	UpdatedAt time.Time `bun:"type:timestamp without time zone,notnull" json:"updated_at"`
}

type AccountStateCode struct {
	ch.CHModel `ch:"account_states_code" json:"-"`

	CodeHash []byte `ch:"type:String"`
	Code     []byte `ch:"type:String"`
}

type AccountStateData struct {
	ch.CHModel `ch:"account_states_data" json:"-"`

	DataHash []byte `ch:"type:String"`
	Data     []byte `ch:"type:String"`
}

func (a *AccountState) BlockID() BlockID {
	return BlockID{
		Workchain: a.Workchain,
		Shard:     a.Shard,
		SeqNo:     a.BlockSeqNo,
	}
}

type LatestAccountState struct {
	bun.BaseModel `bun:"table:latest_account_states" json:"-"`

	Address      addr.Address  `bun:"type:bytea,pk,notnull" json:"address"`
	LastTxLT     uint64        `bun:"type:bigint,notnull" json:"last_tx_lt"`
	AccountState *AccountState `bun:"rel:has-one,join:address=address,join:last_tx_lt=last_tx_lt" json:"account"`
}

func SkipAddress(a addr.Address) bool {
	switch a.Base64() {
	case "EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c": // burn address
		return true
	case "Ef8AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAADAU": // system contract
		return true
	case "Ef8zMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzMzM0vF": // elector contract
		return true
	case "Ef80UXx731GHxVr0-LYf3DIViMerdo3uJLAG3ykQZFjXz2kW": // log tests contract
		return true
	case "Ef9VVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVVbxn": // config contract
		return true
	case "EQAHI1vGuw7d4WG-CtfDrWqEPNtmUuKjKFEFeJmZaqqfWTvW": // BSC Bridge Collector
		return true
	case "EQCuzvIOXLjH2tv35gY4tzhIvXCqZWDuK9kUhFGXKLImgxT5": // ETH Bridge Collector
		return true
	case "EQA2u5Z5Fn59EUvTI-TIrX8PIGKQzNj3qLixdCPPujfJleXC",
		"EQA2Pnxp0rMB9L6SU2z1VqfMIFIfutiTjQWFEXnwa_zPh0P3",
		"EQDhIloDu1FWY9WFAgQDgw0RjuT5bLkf15Rmd5LCG3-0hyoe": // strange heavy testnet address
		return true
	case "EQAWBIxrfQDExJSfFmE5UL1r9drse0dQx_eaV8w9S77VK32F": // tongo emulator segmentation fault
		return true
	default:
		return false
	}
}

type AccountRepository interface {
	AddAddressLabel(context.Context, *AddressLabel) error
	GetAddressLabel(context.Context, addr.Address) (*AddressLabel, error)

	AddAccountStates(ctx context.Context, states []*AccountState) error
	UpdateAccountStates(ctx context.Context, states []*AccountState) error

	// MatchStatesByInterfaceDesc returns (address, last_tx_lt) pairs for suitable account states.
	MatchStatesByInterfaceDesc(ctx context.Context,
		contractName abi.ContractName,
		addresses []*addr.Address,
		codeHash []byte,
		getMethodHashes []int32,
		afterAddress *addr.Address,
		afterTxLt uint64,
		limit int) ([]*AccountStateID, error)

	// GetAllAccountInterfaces returns transaction LT, on which contract interface was updated.
	// It also considers, that contract can be both upgraded and downgraded.
	GetAllAccountInterfaces(context.Context, addr.Address) (map[uint64][]abi.ContractName, error)

	// GetAllAccountStates is pretty much similar to GetAllAccountInterfaces, but it returns updates of code or data.
	GetAllAccountStates(ctx context.Context, a addr.Address, beforeTxLT uint64, limit int) ([]*AccountState, error)
}

//#region Proto

// ToProto converts AccountState struct to its protobuf representation
func (a *AccountState) ToProto() (*desc.AccountState, error) {
	if a == nil {
		return nil, nil
	}

	label, err := a.Label.ToProto()
	if err != nil {
		return nil, err
	}

	return &desc.AccountState{
		Address:    &desc.Address{Value: a.Address[:]},
		Label:      label,
		Workchain:  a.Workchain,
		Shard:      a.Shard,
		BlockSeqNo: a.BlockSeqNo,
		IsActive:   a.IsActive,
		/*Status:          desc.AccountStatus(desc.AccountStatus_value[string(a.Status)]),
		Balance:         a.Balance.String(),
		LastTxLt:        a.LastTxLT,
		LastTxHash:      a.LastTxHash,
		StateHash:       a.StateHash,
		Code:            a.Code,
		CodeHash:        a.CodeHash,
		Data:            a.Data,
		DataHash:        a.DataHash,
		Libraries:       a.Libraries,
		GetMethodHashes: a.GetMethodHashes,
		Types: lo.Map(a.Types, func(item abi.ContractName, _ int) string {
			return string(item)
		}),
		OwnerAddress:  &desc.Address{Value: a.OwnerAddress[:]},
		MinterAddress: &desc.Address{Value: a.MinterAddress[:]},
		Fake:          a.Fake,
		//ExecutedGetMethods: mapAbiMethods(a.ExecutedGetMethods),
		NftContentData: a.NFTContentData.ToProto(),
		FtWalletData:   a.FTWalletData.ToProto(),
		UpdatedAt:      a.UpdatedAt.Format(time.RFC3339),*/
	}, nil
}

func (l *AddressLabel) ToProto() (*desc.AddressLabel, error) {
	if l == nil {
		return nil, nil
	}

	var err error
	labelCategories := lo.Map(l.Categories, func(item LabelCategory, index int) desc.LabelCategory {
		switch item {
		case CentralizedExchange:
			return desc.LabelCategory_CENTRALIZED_EXCHANGE
		case Scam:
			return desc.LabelCategory_SCAM
		default:
			err = fmt.Errorf("failed to parse label category: %v", item)
			return desc.LabelCategory_CENTRALIZED_EXCHANGE
		}
	})
	if err != nil {
		return nil, err
	}

	return &desc.AddressLabel{
		Address:    &desc.Address{Value: l.Address[:]},
		Name:       l.Name,
		Categories: labelCategories,
	}, nil
}

func (l *AddressLabel) FromProto(proto *desc.AddressLabel) error {
	if proto == nil {
		return nil
	}

	var err error
	labelCategories := lo.Map(proto.Categories, func(item desc.LabelCategory, index int) LabelCategory {
		switch item {
		case desc.LabelCategory_CENTRALIZED_EXCHANGE:
			return CentralizedExchange
		case desc.LabelCategory_SCAM:
			return Scam
		default:
			err = fmt.Errorf("failed to parse label category: %v", item)
			return CentralizedExchange
		}
	})
	if err != nil {
		return err
	}

	l.Address = addr.Address(proto.Address.Value)
	l.Name = proto.Name
	l.Categories = labelCategories

	return nil
}

func (n NFTContentData) ToProto() *desc.NFTContentData {
	return &desc.NFTContentData{
		ContentUri:         n.ContentURI,
		ContentName:        n.ContentName,
		ContentDescription: n.ContentDescription,
		ContentImage:       n.ContentImage,
		ContentImageData:   n.ContentImageData,
	}
}

func (f FTWalletData) ToProto() *desc.FTWalletData {
	return &desc.FTWalletData{
		JettonBalance: f.JettonBalance.String(),
	}
}

// fromProto converts protobuf AccountState to Go struct
func fromProto(proto *desc.AccountState) (*AccountState, error) {
	if proto == nil {
		return nil, nil
	}
	/*balance, err := strconv.ParseInt(proto.Balance, 10, 64)
	if err != nil {
		return nil, err
	}*/

	addressLabel := &AddressLabel{}
	err := addressLabel.FromProto(proto.Label)
	if err != nil {
		return nil, err
	}

	return &AccountState{
		Address:    addr.Address(proto.Address.GetValue()),
		Label:      addressLabel,
		Workchain:  proto.Workchain,
		Shard:      proto.Shard,
		BlockSeqNo: proto.BlockSeqNo,
		IsActive:   proto.IsActive,
		//Status:          AccountStatus(proto.Status.String()),
		//Balance:         bunbig.FromInt64(balance),
		//LastTxLT:        proto.LastTxLt,
		//LastTxHash:      proto.LastTxHash,
		//StateHash:       proto.StateHash,
		//Code:            proto.Code,
		//CodeHash:        proto.CodeHash,
		//Data:            proto.Data,
		//DataHash:        proto.DataHash,
		//Libraries:       proto.Libraries,
		//GetMethodHashes: proto.GetMethodHashes,
		//Types:           proto.Types,
		//OwnerAddress:    BytesToAddress(proto.OwnerAddress.GetValue()),
		//MinterAddress:   BytesToAddress(proto.MinterAddress.GetValue()),
		//Fake:            proto.Fake,
		////ExecutedGetMethods: unmapAbiMethods(proto.ExecutedGetMethods),
		//NFTContentData: FromProtoNFT(proto.NftContentData),
		//FTWalletData:   FromProtoFT(proto.FtWalletData),
		//UpdatedAt:      parseTime(proto.UpdatedAt),
	}, nil
}

//func FromProtoLabel(proto *desc.AddressLabel) *AddressLabel {
//	if proto == nil {
//		return nil
//	}
//	return &AddressLabel{
//		Address:    BytesToAddress(proto.Address.GetValue()),
//		Name:       proto.Name,
//		Categories: reverseLabelCategories(proto.Categories),
//	}
//}
//
//func BytesToAddress(b []byte) addr.Address {
//	var address addr.Address
//	copy(address[:], b)
//	return address
//}
//
//func parseTime(t string) time.Time {
//	parsed, _ := time.Parse(time.RFC3339, t)
//	return parsed
//}

/*func mapAbiMethods(m map[abi.ContractName][]abi.GetMethodExecution) map[string][]string {
	res := make(map[string][]string)
	for k, v := range m {
		var executions []string
		for _, e := range v {
			executions = append(executions, e.String())
		}
		res[string(k)] = executions
	}
	return res
}

func unmapAbiMethods(m map[string][]string) map[abi.ContractName][]abi.GetMethodExecution {
	res := make(map[abi.ContractName][]abi.GetMethodExecution)
	for k, v := range m {
		var executions []abi.GetMethodExecution
		for _, e := range v {
			executions = append(executions, abi.GetMethodExecutionFromString(e))
		}
		res[abi.ContractName(k)] = executions
	}
	return res
}
*/
//#endregion
