package keeper

import (
	"fmt"

	"github.com/cometbft/cometbft/libs/log"
	"github.com/cosmos/cosmos-sdk/codec"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/lavanet/lava/x/fixationstore"
	fixationtypes "github.com/lavanet/lava/x/fixationstore/types"
	"github.com/lavanet/lava/x/timerstore"

	"github.com/lavanet/lava/x/dualstaking/types"
)

type (
	Keeper struct {
		cdc        codec.BinaryCodec
		storeKey   storetypes.StoreKey
		memKey     storetypes.StoreKey
		paramstore paramtypes.Subspace

		bankKeeper         types.BankKeeper
		accountKeeper      types.AccountKeeper
		epochstorageKeeper types.EpochstorageKeeper
		specKeeper         types.SpecKeeper

		delegationFS fixationstore.FixationStore // map proviers/chainID -> delegations
		delegatorFS  fixationstore.FixationStore // map delegators -> providers
		unbondingTS  timerstore.TimerStore       // track unbonding timeouts
	}
)

func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey,
	memKey storetypes.StoreKey,
	ps paramtypes.Subspace,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	epochstorageKeeper types.EpochstorageKeeper,
	specKeeper types.SpecKeeper,
	fixationStoreKeeper types.FixationStoreKeeper,
	timerStoreKeeper types.TimerStoreKeeper,
) *Keeper {
	// set KeyTable if it has not already been set
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(types.ParamKeyTable())
	}

	keeper := &Keeper{
		cdc:        cdc,
		storeKey:   storeKey,
		memKey:     memKey,
		paramstore: ps,

		bankKeeper:         bankKeeper,
		accountKeeper:      accountKeeper,
		epochstorageKeeper: epochstorageKeeper,
		specKeeper:         specKeeper,
	}

	// ensure bonded and not bonded module accounts are set
	if addr := accountKeeper.GetModuleAddress(types.BondedPoolName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.BondedPoolName))
	}
	if addr := accountKeeper.GetModuleAddress(types.NotBondedPoolName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.NotBondedPoolName))
	}

	delegationFS := *fixationStoreKeeper.NewFixationStore(storeKey, types.DelegationPrefix)
	delegatorFS := *fixationStoreKeeper.NewFixationStore(storeKey, types.DelegatorPrefix)

	timerCallback := func(ctx sdk.Context, key, data []byte) {
		keeper.finalizeUnbonding(ctx, key, data)
	}

	unbondingTS := *timerStoreKeeper.NewTimerStore(storeKey, types.UnbondingPrefix).
		WithCallbackByBlockHeight(timerCallback)

	keeper.delegationFS = delegationFS
	keeper.delegatorFS = delegatorFS
	keeper.unbondingTS = unbondingTS

	return keeper
}

// ExportDelegations exports dualstaking delegations data (for genesis)
func (k Keeper) ExportDelegations(ctx sdk.Context) fixationtypes.GenesisState {
	return k.delegationFS.Export(ctx)
}

// ExportDelegators exports dualstaking delegators data (for genesis)
func (k Keeper) ExportDelegators(ctx sdk.Context) fixationtypes.GenesisState {
	return k.delegatorFS.Export(ctx)
}

// ExportUnbondings exports dualstaking unbonding timers data (for genesis)
func (k Keeper) ExportUnbondings(ctx sdk.Context) []fixationtypes.RawMessage {
	return k.unbondingTS.Export(ctx)
}

// InitDelegations imports dualstaking delegations data (from genesis)
func (k Keeper) InitDelegations(ctx sdk.Context, data fixationtypes.GenesisState) {
	k.delegationFS.Init(ctx, data)
}

// InitDelegators imports dualstaking delegators data (from genesis)
func (k Keeper) InitDelegators(ctx sdk.Context, data fixationtypes.GenesisState) {
	k.delegatorFS.Init(ctx, data)
}

// InitUnbondings imports subscriptions timers data (from genesis)
func (k Keeper) InitUnbondings(ctx sdk.Context, data []fixationtypes.RawMessage) {
	k.unbondingTS.Init(ctx, data)
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}
