package leaderreward

import (
	"math/big"

	"github.com/matrix/go-matrix/common"
	"github.com/matrix/go-matrix/log"
	"github.com/matrix/go-matrix/params/manparams"
)

const (
	PackageName = "leader奖励"
)

type LeaderReward struct {
}

func (lr *LeaderReward) SetLeaderRewards(reward *big.Int, Leader common.Address, num uint64) map[common.Address]*big.Int {
	//广播区块不给验证者发钱
	if manparams.IsBroadcastNumber(num) {
		return nil
	}
	if reward.Cmp(big.NewInt(0)) <= 0 {
		log.WARN(PackageName, "奖励金额不合法", reward)
		return nil
	}
	if Leader.Equal(common.Address{}) {
		log.ERROR(PackageName, "奖励的地址非法", Leader.Hex())
		return nil
	}
	rewards := make(map[common.Address]*big.Int)
	rewards[Leader] = reward
	log.INFO(PackageName, "leader 奖励地址", Leader, "奖励金额", reward)
	return rewards
}
