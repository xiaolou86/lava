package fixationstore

import (
	"github.com/cosmos/cosmos-sdk/codec"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lavanet/lava/x/timerstore"
)

func NewKeeper(cdc codec.BinaryCodec, tsKeeper *timerstore.Keeper) *Keeper {
	return &Keeper{
		cdc: cdc,
		ts:  tsKeeper,
	}
}

// Keeper is the fixationstore keeper. The keeper retains all the fixation stores used by modules,
// it also manages their lifecycle.
type Keeper struct {
	fixationsStores []*FixationStore
	ts              *timerstore.Keeper
	cdc             codec.BinaryCodec
}

func (k *Keeper) NewFixationStore(storeKey storetypes.StoreKey, prefix string) *FixationStore {
	ts := k.ts.NewTimerStore(storeKey, prefix)
	fs := NewFixationStore(storeKey, k.cdc, prefix, ts)
	k.fixationsStores = append(k.fixationsStores, fs)
	return fs
}

func (k *Keeper) BeginBlock(ctx sdk.Context) {}
