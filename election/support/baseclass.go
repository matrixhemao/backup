// Copyright (c) 2018 The MATRIX Authors
// Distributed under the MIT software license, see the accompanying
// file COPYING or or http://www.opensource.org/licenses/mit-license.php
package support

import (
	"fmt"
	"github.com/matrix/go-matrix/common"
	"github.com/matrix/go-matrix/core/vm"
	"github.com/matrix/go-matrix/mc"
	"math/big"
	"math/rand"
)

type RatioList struct {
	MinNum uint64
	Ratio  float64
}
type Pnormalized struct {
	Value float64
	Addr  common.Address
}
type AllNative struct {
	Master    []mc.ElectNodeInfo //验证者主节点
	BackUp    []mc.ElectNodeInfo //验证者备份
	Candidate []mc.ElectNodeInfo //验证者候选

	MasterQ    []common.Address //第一梯队候选
	BackUpQ    []common.Address //第二梯队候选
	CandidateQ []common.Address //第三梯队候选
	ElectInfo  *mc.ElectConfigInfo
}

type Strallyint struct {
	Value int
	Addr  common.Address
	VIPLevel common.VIPRoleType
}

type Node struct {
	Address    common.Address
	Deposit    *big.Int
	WithdrawH  *big.Int
	OnlineTime *big.Int
	Ratio      uint16
	vipLevel   common.VIPRoleType
	index      int
	Usable     bool
}

type Electoion struct {
	SeqNum        uint64
	RandSeed      *big.Int
	VipLevelCfg   []mc.VIPConfig
	NodeList      []Node

	EleCfg        mc.ElectConfigInfo_All

	ChosedNum int
	NeedNum int
	HasChosedNode [][]Strallyint
	MapMoney map[common.Address]uint64
}

func (node *Node) SetUsable(status bool) {
	node.Usable = status
}

func (node *Node) SetIndex(index int) {
	node.index = index
}
func (node *Node) SetVipLevelInfo(VipLevelCfg []mc.VIPConfig)uint64 {
	temp := big.NewInt(0).Set(node.Deposit)
	deposMan := temp.Div(temp, common.ManValue).Uint64()


	for index:=len(VipLevelCfg)-1;index>=0;index--{
		if deposMan>=VipLevelCfg[index].MinMoney{
			node.vipLevel=common.GetVIPLevel(index)
			node.Ratio=VipLevelCfg[index].StockScale
			return deposMan
		}
	}
	node.Ratio = DefaultRatio
	node.vipLevel = common.VIP_Nil
	return deposMan
}

func (node *Node) SetDepositInfo(depsit vm.DepositDetail) {
	node.Address = depsit.Address
	node.OnlineTime = depsit.OnlineTime
	node.WithdrawH = depsit.WithdrawH
	node.Deposit = depsit.Deposit

	if nil == depsit.Deposit {
		node.Deposit = big.NewInt(DefaultNodeConfig)
	}
	if nil == depsit.WithdrawH {
		node.WithdrawH = big.NewInt(DefaultNodeConfig)
	}
	if nil == depsit.OnlineTime {
		node.OnlineTime = big.NewInt(DefaultNodeConfig)
	}
}

func NewElelection(VipLevelCfg []mc.VIPConfig, vm []vm.DepositDetail, EleCfg mc.ElectConfigInfo_All, randseed *big.Int, seqNum uint64,types common.RoleType) *Electoion {
	var vip Electoion
	vip.SeqNum = seqNum
	vip.RandSeed = randseed
	vip.EleCfg = EleCfg

	vip.VipLevelCfg = VipLevelCfg
	vip.ChosedNum=0
	switch types {
	case common.RoleValidator:
		vip.NeedNum=int(EleCfg.BackValidator+EleCfg.ValidatorNum)
	default:
		vip.NeedNum=int(EleCfg.MinerNum)
	}
	vip.MapMoney=make(map[common.Address]uint64)

	for i := 0; i < len(vm); i++ {
		vip.NodeList = append(vip.NodeList, Node{})
	}
	for i := 0; i < len(vm); i++ {
		vip.NodeList[i].SetDepositInfo(vm[i])
		manValue:=vip.NodeList[i].SetVipLevelInfo(VipLevelCfg)
		vip.NodeList[i].SetIndex(i)
		vip.NodeList[i].SetUsable(true)
		vip.MapMoney[vip.NodeList[i].Address]=manValue

	}
	return &vip
}

func FindAddress(addr common.Address, addrList []common.Address) bool {
	for _, v := range addrList {
		if v.Equal(addr) == true {
			return true
		}
	}
	return false
}
func (vip *Electoion) DisPlayNode() {
	for _, v := range vip.NodeList {
		fmt.Println(v.Address, v.Deposit, v.WithdrawH, v.OnlineTime, v.vipLevel, v.index, "Ratio", v.Ratio,v.Usable)
	}
}
func (vip *Electoion)SetChosed(node []Strallyint){
	ChoseNode:=[]common.Address{}
	for _,v:=range node{
		ChoseNode=append(ChoseNode,v.Addr)
	}
	for k,v:=range vip.NodeList{
		if FindAddress(v.Address,ChoseNode){
			vip.NodeList[k].SetUsable(false)
		}
	}
	vip.ChosedNum+=len(node)
	vip.HasChosedNode=append(vip.HasChosedNode,node)
}
func (vip *Electoion) ProcessBlackNode() {
	for k, v := range vip.NodeList {
		if FindAddress(v.Address, vip.EleCfg.BlackList) {
			vip.NodeList[k].SetUsable(false)
		}
	}
}

func (vip *Electoion)GetVipStock(addr common.Address)int{
	stockSum:=int(0)
	stockDespoit:=uint64(0)
	for k,v:=range vip.HasChosedNode{
		if k!=len(vip.HasChosedNode)-1{
			continue
		}
		for _,vv:=range v{
			stockSum+=vv.Value
			stockDespoit+=vip.MapMoney[vv.Addr]
		}
	}
	if float64(stockSum)/float64(stockDespoit)==0{
		return int(vip.MapMoney[addr]/vip.VipLevelCfg[1].MinMoney)

	}else{
		ratio:=float64(stockSum)/float64(stockDespoit)*float64(vip.MapMoney[addr])
		return int(ratio)
	}


}
func (vip *Electoion) ProcessWhiteNode() {
	/*
	for k, v := range vip.NodeList {
		if v.Usable == false {
			continue
		}
		if FindAddress(v.Address, vip.EleCfg.WhiteList) {
			vip.WhiteNodeInfo = append(vip.WhiteNodeInfo, Strallyint{Addr: v.Address, Value: DefaultStock})
			vip.NodeList[k].SetUsable(false)
		}
	}
	*/
}
func (vip *Electoion) GetNodeByLevel(level common.VIPRoleType) []Node {
	specialNode := make([]Node, 0)
	for i := 0; i < len(vip.NodeList); i++ {
		if vip.NodeList[i].Usable == false {
			continue
		}
		if level == vip.NodeList[i].vipLevel {
			specialNode = append(specialNode, vip.NodeList[i])
		}
	}
	return specialNode
}

func (vip *Electoion) GetNodeIndexByLevel(level common.VIPRoleType) []int {
	specialNode := make([]int, 0)
	for i := 0; i < len(vip.NodeList); i++ {
		if level == vip.NodeList[i].vipLevel {
			specialNode = append(specialNode, i)
		}
	}
	return specialNode
}

func (vip *Electoion) GetLastNode() []Node {
	var remainNodeList = make([]Node, 0)
	for i := 0; i < len(vip.NodeList); i++ {
		if vip.NodeList[i].Usable == false {
			continue
		}
		remainNodeList = append(remainNodeList, vip.NodeList[i])
	}
	return remainNodeList
}

func (vip *Electoion) GetWeight(role common.RoleType) []Pnormalized {
	lastnode := vip.GetLastNode()
	return CalcValue(lastnode, role)
}



func (vip *Electoion) ValidatorTopGen(MasterNum int, BackupNum int) ([]Strallyint, []Strallyint, []Strallyint) {
	weight := vip.GetWeight(common.RoleValidator)
	switch DefalutValidatorElectPlug {
	case ValidatorElectPlug_Direct:
		return getValidator_Direct(weight, MasterNum, BackupNum, vip.RandSeed)
	default:
		return getValidator_Order(weight, MasterNum, BackupNum, vip.RandSeed)
	}
}

func Knuth_Fisher_Yates_Algorithm(nodeList []Node, randSeed *big.Int) []Node {
	//高纳德置乱算法
	rand.Seed(randSeed.Int64())
	for index := len(nodeList) - 1; index > 0; index-- {
		aimIndex := rand.Intn(index + 1)
		t := nodeList[index]
		nodeList[index] = nodeList[aimIndex]
		nodeList[aimIndex] = t
	}
	return nodeList
}
func (vip *Electoion) GetIndex(addr common.Address) (int, bool) {
	for k, v := range vip.NodeList {
		if v.Address.Equal(addr) {
			return k, true
		}
	}
	return 0, false
}

type SortNodeList []Node

func (self SortNodeList) Len() int {
	return len(self)
}
func (self SortNodeList) Less(i, j int) bool {
	if self[i].Deposit.Cmp(self[j].Deposit) == 0 {
		return self[i].OnlineTime.Cmp(self[j].OnlineTime) > 0
	}
	return self[i].Deposit.Cmp(self[j].Deposit) > 0
}
func (self SortNodeList) Swap(i, j int) {
	temp := self[i]
	self[i] = self[j]
	self[j] = temp
}
