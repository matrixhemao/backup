// Copyright (c) 2018 The MATRIX Authors
// Distributed under the MIT software license, see the accompanying
// file COPYING or or http://www.opensource.org/licenses/mit-license.php

package leaderelect

import (
	"github.com/matrix/go-matrix/common"
	"github.com/matrix/go-matrix/core"
	"github.com/matrix/go-matrix/log"
	"github.com/matrix/go-matrix/mc"
	"github.com/matrix/go-matrix/params/manparams"
	"github.com/pkg/errors"
)

type leaderCalculator struct {
	number      uint64
	preLeader   common.Address
	preHash     common.Hash
	preIsSupper bool
	leaderList  map[uint32]common.Address
	validators  []mc.TopologyNodeInfo
	specials    *mc.MatrixSpecialAccounts
	chain       *core.BlockChain
	logInfo     string
}

func newLeaderCalculator(chain *core.BlockChain, number uint64, logInfo string) *leaderCalculator {
	return &leaderCalculator{
		number:      number,
		preLeader:   common.Address{},
		preHash:     common.Hash{},
		preIsSupper: false,
		leaderList:  make(map[uint32]common.Address),
		validators:  nil,
		specials:    nil,
		chain:       chain,
		logInfo:     logInfo,
	}
}

func (self *leaderCalculator) SetValidatorsAndSpecials(preHash common.Hash, preIsSupper bool, preLeader common.Address, validators []mc.TopologyNodeInfo, specials *mc.MatrixSpecialAccounts, bcInterval *manparams.BCInterval) error {
	if validators == nil || specials == nil || bcInterval == nil {
		return ErrValidatorsIsNil
	}

	preNumber := self.number - 1
	realPreLeader := preLeader
	if preIsSupper == false && bcInterval.IsBroadcastNumber(preNumber) && preNumber != 0 {
		headerHash, err := self.chain.GetAncestorHash(preHash, preNumber-1)
		if err != nil {
			return errors.Errorf("获取广播区块OR超级区块前一区块(%d)错误!", preNumber-1, "err", err)
		}
		header := self.chain.GetHeaderByHash(headerHash)
		if header == nil {
			return errors.Errorf("获取广播区块OR超级区块前一区块(%s)错误!", headerHash.TerminalString())
		}
		realPreLeader = header.Leader
	}
	log.INFO(self.logInfo, "计算leader列表", "开始", "preLeader", realPreLeader.Hex(), "前一个区块是否为超级区块", preIsSupper)
	leaderList, err := calLeaderList(realPreLeader, preNumber, preIsSupper, validators, bcInterval)
	if err != nil {
		return err
	}
	self.leaderList = leaderList
	self.preLeader.Set(preLeader)
	self.preHash.Set(preHash)
	self.validators = validators
	self.preIsSupper = preIsSupper
	self.specials = specials

	return nil
}

func (self *leaderCalculator) GetValidators() (*mc.TopologyGraph, error) {
	if len(self.validators) == 0 {
		return nil, errors.New("验证者列表为空")
	}
	rlt := &mc.TopologyGraph{}
	for i := 0; i < len(self.validators); i++ {
		rlt.NodeList = append(rlt.NodeList, self.validators[i])
	}
	return rlt, nil
}

func (self *leaderCalculator) GetLeader(turn uint32, bcInterval *manparams.BCInterval) (*leaderData, error) {
	if bcInterval == nil {
		return nil, errors.New("leader calculator: param bcInterval is nil")
	}
	leaderCount := uint32(len(self.leaderList))
	if leaderCount == 0 {
		return nil, ErrValidatorsIsNil
	}
	if self.specials == nil {
		return nil, ErrSepcialsIsNil
	}

	leaders := &leaderData{}
	number := self.number
	if bcInterval.IsReElectionNumber(number) {
		leaders.leader.Set(self.specials.BroadcastAccount.Address)
		leaders.nextLeader.Set(self.leaderList[turn%leaderCount])
		return leaders, nil
	}

	if bcInterval.IsBroadcastNumber(number) {
		leaders.leader.Set(self.specials.BroadcastAccount.Address)
		leaders.nextLeader.Set(self.leaderList[(turn)%leaderCount])
		return leaders, nil
	}

	leaders.leader.Set(self.leaderList[turn%leaderCount])
	if bcInterval.IsBroadcastNumber(number + 1) {
		leaders.nextLeader.Set(self.specials.BroadcastAccount.Address)
	} else {
		leaders.nextLeader.Set(self.leaderList[(turn+1)%leaderCount])
	}
	return leaders, nil
}

func calLeaderList(preLeader common.Address, preNumber uint64, preIsSupper bool, validators []mc.TopologyNodeInfo, bcInterval *manparams.BCInterval) (map[uint32]common.Address, error) {
	ValidatorNum := len(validators)
	var startPos = 0
	if preIsSupper || bcInterval.IsReElectionNumber(preNumber) || bcInterval.IsReElectionNumber(preNumber+1) {
		startPos = 0
	} else {
		preIndex, err := findLeaderIndex(preLeader, validators)
		if err != nil {
			return nil, err
		}
		startPos = preIndex + 1
	}
	leaderList := make(map[uint32]common.Address)
	for i := 0; i < ValidatorNum; i++ {
		leaderList[uint32(i)] = validators[(startPos+int(i))%ValidatorNum].Account
	}
	return leaderList, nil
}

func findLeaderIndex(preLeader common.Address, validators []mc.TopologyNodeInfo) (int, error) {
	for index, v := range validators {
		if v.Account == preLeader {
			return index, nil
		}
	}
	return 0, ErrValidatorNotFound
}
