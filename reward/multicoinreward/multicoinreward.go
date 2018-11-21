package multicoinreward

import (
	"github.com/matrix/go-matrix/reward"
	"github.com/matrix/go-matrix/reward/cfg"
	"github.com/matrix/go-matrix/reward/rewardexec"
	"github.com/matrix/go-matrix/reward/util"
)

const (
PackageName = "多币种奖励"

//todo: 分母10000， 加法做参数检查
ValidatorsTxsRewardRate = uint64(util.RewardFullRate) //验证者交易奖励比例100%
MinerTxsRewardRate      = uint64(0)                   //矿工交易奖励比例0%
FoundationTxsRewardRate = uint64(0)                   //基金会交易奖励比例0%

MinerOutRewardRate     = uint64(4000) //出块矿工奖励40%
ElectedMinerRewardRate = uint64(6000) //当选矿工奖励60%

LeaderRewardRate            = uint64(4000) //出块验证者（leader）奖励40%
ElectedValidatorsRewardRate = uint64(6000) //当选验证者奖励60%

OriginElectOfflineRewardRate = uint64(util.RewardFullRate) //初选下线验证者奖励50%
BackupRate                   = uint64(0) //当前替补验证者奖励50%

)


func New(chain util.ChainReader) reward.Reward {

	RewardMount := &cfg.RewardMountCfg{
		MinersRate:     MinerTxsRewardRate,
		ValidatorsRate: ValidatorsTxsRewardRate,

		MinerOutRate:     MinerOutRewardRate,
		ElectedMinerRate: ElectedMinerRewardRate,
		FoundationMinerRate: FoundationTxsRewardRate,

		LeaderRate:            LeaderRewardRate,
		ElectedValidatorsRate: ElectedValidatorsRewardRate,
		FoundationValidatorRate:FoundationTxsRewardRate,
		OriginElectOfflineRate: OriginElectOfflineRewardRate,
		BackupRewardRate:       BackupRate,
	}
	rewardCfg := cfg.New(RewardMount, nil)
	return rewardexec.New(chain, rewardCfg)
}
