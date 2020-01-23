package precompute

import (
	"github.com/pkg/errors"

	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// ProcessRewardsAndPenaltiesPrecompute processes the rewards and penalties of individual validator.
// This is an optimized version by passing in precomputed validator attesting records and and total epoch balances.
func ProcessRewardsAndPenaltiesPrecompute(
	state *stateTrie.BeaconState,
	bp *Balance,
	vp []*Validator,
) error {
	// Can't process rewards and penalties in genesis epoch.
	if helpers.CurrentEpoch(state) == 0 {
		return nil
	}

	numVals := state.NumofValidators()
	numBalances := state.NumBalances()
	// Guard against an out-of-bounds using validator balance precompute.
	if len(vp) != numVals || len(vp) != numBalances {
		return errors.New("precomputed registries not the same length as state registries")
	}

	attsRewards, attsPenalties, err := attestationDeltas(state, bp, vp)
	if err != nil {
		return errors.Wrap(err, "could not get attestation delta")
	}
	proposerRewards, err := proposerDeltaPrecompute(state, bp, vp)
	if err != nil {
		return errors.Wrap(err, "could not get attestation delta")
	}
	for i := 0; i < numVals; i++ {
		if err := helpers.IncreaseBalance(state, uint64(i), attsRewards[i]+proposerRewards[i]); err != nil {
			return err
		}
		if err := helpers.DecreaseBalance(state, uint64(i), attsPenalties[i]); err != nil {
			return err
		}
	}
	return nil
}

// This computes the rewards and penalties differences for individual validators based on the
// voting records.
func attestationDeltas(state *stateTrie.BeaconState, bp *Balance, vp []*Validator) ([]uint64, []uint64, error) {
	rewards := make([]uint64, state.NumofValidators())
	penalties := make([]uint64, state.NumofValidators())

	for i, v := range vp {
		rewards[i], penalties[i] = attestationDelta(state, bp, v)
	}
	return rewards, penalties, nil
}

func attestationDelta(state *stateTrie.BeaconState, bp *Balance, v *Validator) (uint64, uint64) {
	eligible := v.IsActivePrevEpoch || (v.IsSlashed && !v.IsWithdrawableCurrentEpoch)
	if !eligible {
		return 0, 0
	}

	e := helpers.PrevEpoch(state)
	vb := v.CurrentEpochEffectiveBalance
	br := vb * params.BeaconConfig().BaseRewardFactor / mathutil.IntegerSquareRoot(bp.CurrentEpoch) / params.BeaconConfig().BaseRewardsPerEpoch
	r, p := uint64(0), uint64(0)

	// Process source reward / penalty
	if v.IsPrevEpochAttester && !v.IsSlashed {
		r += br * bp.PrevEpochAttesters / bp.CurrentEpoch
		proposerReward := br / params.BeaconConfig().ProposerRewardQuotient
		maxAtteserReward := br - proposerReward
		r += maxAtteserReward / v.InclusionDistance
	} else {
		p += br
	}

	// Process target reward / penalty
	if v.IsPrevEpochTargetAttester && !v.IsSlashed {
		r += br * bp.PrevEpochTargetAttesters / bp.CurrentEpoch
	} else {
		p += br
	}

	// Process head reward / penalty
	if v.IsPrevEpochHeadAttester && !v.IsSlashed {
		r += br * bp.PrevEpochHeadAttesters / bp.CurrentEpoch
	} else {
		p += br
	}

	// Process finality delay penalty
	var finalizedEpoch uint64
	if state.FinalizedCheckpoint() != nil {
		finalizedEpoch = state.FinalizedCheckpoint().Epoch
	}
	finalityDelay := e - finalizedEpoch
	if finalityDelay > params.BeaconConfig().MinEpochsToInactivityPenalty {
		p += params.BeaconConfig().BaseRewardsPerEpoch * br
		if !v.IsPrevEpochTargetAttester {
			p += vb * finalityDelay / params.BeaconConfig().InactivityPenaltyQuotient
		}
	}
	return r, p
}

// This computes the rewards and penalties differences for individual validators based on the
// proposer inclusion records.
func proposerDeltaPrecompute(state *stateTrie.BeaconState, bp *Balance, vp []*Validator) ([]uint64, error) {
	rewards := make([]uint64, state.NumofValidators())

	totalBalance := bp.CurrentEpoch

	for _, v := range vp {
		if v.IsPrevEpochAttester {
			vBalance := v.CurrentEpochEffectiveBalance
			baseReward := vBalance * params.BeaconConfig().BaseRewardFactor / mathutil.IntegerSquareRoot(totalBalance) / params.BeaconConfig().BaseRewardsPerEpoch
			proposerReward := baseReward / params.BeaconConfig().ProposerRewardQuotient
			rewards[v.ProposerIndex] += proposerReward
		}
	}
	return rewards, nil
}
