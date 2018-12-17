// Copyright (c) 2018 The MATRIX Authors
// Distributed under the MIT software license, see the accompanying
// file COPYING or or http://www.opensource.org/licenses/mit-license.php
package matrixwork

import (
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/matrix/go-matrix/baseinterface"
	"github.com/matrix/go-matrix/params/manparams"

	"github.com/matrix/go-matrix/reward/blkreward"
	"github.com/matrix/go-matrix/reward/interest"
	"github.com/matrix/go-matrix/reward/lottery"
	"github.com/matrix/go-matrix/reward/slash"
	"github.com/matrix/go-matrix/reward/txsreward"

	"github.com/matrix/go-matrix/ca"
	"github.com/matrix/go-matrix/depoistInfo"
	"github.com/matrix/go-matrix/mc"

	"sort"
	"sync"

	"strings"

	"github.com/matrix/go-matrix/accounts/abi"
	"github.com/matrix/go-matrix/common"
	"github.com/matrix/go-matrix/common/hexutil"
	"github.com/matrix/go-matrix/core"
	"github.com/matrix/go-matrix/core/state"
	"github.com/matrix/go-matrix/core/types"
	"github.com/matrix/go-matrix/core/vm"
	"github.com/matrix/go-matrix/event"
	"github.com/matrix/go-matrix/log"
	"github.com/matrix/go-matrix/params"
)

type ChainReader interface {
	StateAt(root common.Hash) (*state.StateDB, error)
	GetBlockByHash(hash common.Hash) *types.Block
}

var packagename string = "matrixwork"
var (
	depositDef = ` [{"constant": true,"inputs": [],"name": "getDepositList","outputs": [{"name": "","type": "address[]"}],"payable": false,"stateMutability": "view","type": "function"},
			{"constant": true,"inputs": [{"name": "addr","type": "address"}],"name": "getDepositInfo","outputs": [{"name": "","type": "uint256"},{"name": "","type": "bytes"},{"name": "","type": "uint256"}],"payable": false,"stateMutability": "view","type": "function"},
    		{"constant": false,"inputs": [{"name": "nodeID","type": "bytes"}],"name": "valiDeposit","outputs": [],"payable": true,"stateMutability": "payable","type": "function"},
    		{"constant": false,"inputs": [{"name": "nodeID","type": "bytes"}],"name": "minerDeposit","outputs": [],"payable": true,"stateMutability": "payable","type": "function"},
    		{"constant": false,"inputs": [],"name": "withdraw","outputs": [],"payable": false,"stateMutability": "nonpayable","type": "function"},
    		{"constant": false,"inputs": [],"name": "refund","outputs": [],"payable": false,"stateMutability": "nonpayable","type": "function"},
			{"constant": false,"inputs": [{"name": "addr","type": "address"}],"name": "interestAdd","outputs": [],"payable": true,"stateMutability": "payable","type": "function"},
			{"constant": false,"inputs": [{"name": "addr","type": "address"}],"name": "getinterest","outputs": [],"payable": false,"stateMutability": "payable","type": "function"}]`

	depositAbi, Abierr = abi.JSON(strings.NewReader(depositDef))
)

// Work is the workers current environment and holds
// all of the current state information
type Work struct {
	config *params.ChainConfig
	signer types.Signer

	State *state.StateDB // apply state changes here
	//ancestors *set.Set       // ancestor set (used for checking uncle parent validity)
	//family    *set.Set       // family set (used for checking uncle invalidity)
	//uncles    *set.Set       // uncle set
	tcount  int           // tx count in cycle
	gasPool *core.GasPool // available gas used to pack transactions

	Block *types.Block // the new block

	header   *types.Header
	uptime   map[common.Address]uint64
	random   *baseinterface.Random
	txs      []types.SelfTransaction
	Receipts []*types.Receipt

	createdAt time.Time
}
type coingasUse struct {
	mapcoin  map[string]*big.Int
	mapprice map[string]*big.Int
	mu       sync.RWMutex
}

var mapcoingasUse coingasUse = coingasUse{mapcoin: make(map[string]*big.Int), mapprice: make(map[string]*big.Int)}

func (cu *coingasUse) setCoinGasUse(txer types.SelfTransaction, gasuse uint64) {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	gasAll := new(big.Int).SetUint64(gasuse)
	priceAll := txer.GasPrice()
	if gas, ok := cu.mapcoin[txer.GetTxCurrency()]; ok {
		gasAll = new(big.Int).Add(gasAll, gas)
	}
	cu.mapcoin[txer.GetTxCurrency()] = gasAll

	if _, ok := cu.mapprice[txer.GetTxCurrency()]; !ok {
		if priceAll.Cmp(new(big.Int).SetUint64(params.TxGasPrice)) >= 0 {
			cu.mapprice[txer.GetTxCurrency()] = priceAll
		}
	}
}
func (cu *coingasUse) getCoinGasPrice(typ string) *big.Int {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	price, ok := cu.mapprice[typ]
	if !ok {
		price = new(big.Int).SetUint64(0)
	}
	return price
}
func (cu *coingasUse) getCoinGasUse(typ string) *big.Int {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	gas, ok := cu.mapcoin[typ]
	if !ok {
		gas = new(big.Int).SetUint64(0)
	}
	return gas
}
func (cu *coingasUse) clearmap() {
	cu.mu.Lock()
	defer cu.mu.Unlock()
	cu.mapcoin = make(map[string]*big.Int)
	cu.mapprice = make(map[string]*big.Int)
}
func NewWork(config *params.ChainConfig, bc ChainReader, gasPool *core.GasPool, header *types.Header, random *baseinterface.Random) (*Work, error) {

	Work := &Work{
		config:  config,
		signer:  types.NewEIP155Signer(config.ChainId),
		gasPool: gasPool,
		header:  header,
		uptime:  make(map[common.Address]uint64, 0),
		random:  random,
	}
	var err error

	Work.State, err = bc.StateAt(bc.GetBlockByHash(header.ParentHash).Root())

	if err != nil {
		return nil, err
	}
	return Work, nil
}

//func (env *Work) commitTransactions(mux *event.TypeMux, txs *types.TransactionsByPriceAndNonce, bc *core.BlockChain, coinbase common.Address) (listN []uint32, retTxs []types.SelfTransaction) {
func (env *Work) commitTransactions(mux *event.TypeMux, txser types.SelfTransactions, bc *core.BlockChain, coinbase common.Address) (listret []*common.RetCallTxN, retTxs []types.SelfTransaction) {
	if env.gasPool == nil {
		env.gasPool = new(core.GasPool).AddGas(env.header.GasLimit)
	}

	var coalescedLogs []*types.Log
	tmpRetmap := make(map[byte][]uint32)
	for _, txer := range txser {
		// If we don't have enough gas for any further transactions then we're done
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			break
		}
		if txer.GetTxNLen() == 0 {
			log.Info("file work func commitTransactions err: tx.N is nil")
			continue
		}
		// We use the eip155 signer regardless of the current hf.
		from, _ := txer.GetTxFrom()

		// Start executing the transaction
		env.State.Prepare(txer.Hash(), common.Hash{}, env.tcount)
		err, logs := env.commitTransaction(txer, bc, coinbase, env.gasPool)
		switch err {
		case core.ErrGasLimitReached:
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Trace("Gas limit exceeded for current block", "sender", from)
		case core.ErrNonceTooLow:
			// New head notification data race between the transaction pool and miner, shift
			log.Trace("Skipping transaction with low nonce", "sender", from, "nonce", txer.Nonce())
		case core.ErrNonceTooHigh:
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Trace("Skipping account with hight nonce", "sender", from, "nonce", txer.Nonce())
		case nil:
			// Everything ok, collect the logs and shift in the next transaction from the same account
			if txer.GetTxNLen() != 0 {
				n := txer.GetTxN(0)
				if listN, ok := tmpRetmap[txer.TxType()]; ok {
					listN = append(listN, n)
					tmpRetmap[txer.TxType()] = listN
				} else {
					listN := make([]uint32, 0)
					listN = append(listN, n)
					tmpRetmap[txer.TxType()] = listN
				}
				retTxs = append(retTxs, txer)
			}
			coalescedLogs = append(coalescedLogs, logs...)
			env.tcount++
		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", txer.Hash(), "err", err)
		}
	}
	for t, n := range tmpRetmap {
		ts := common.RetCallTxN{t, n}
		listret = append(listret, &ts)
	}
	if len(coalescedLogs) > 0 || env.tcount > 0 {
		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		go func(logs []*types.Log, tcount int) {
			if len(logs) > 0 {
				mux.Post(core.PendingLogsEvent{Logs: logs})
			}
			if tcount > 0 {
				mux.Post(core.PendingStateEvent{})
			}
		}(cpy, env.tcount)
	}
	return listret, retTxs
}

func (env *Work) commitTransaction(tx types.SelfTransaction, bc *core.BlockChain, coinbase common.Address, gp *core.GasPool) (error, []*types.Log) {
	snap := env.State.Snapshot()
	receipt, _, err := core.ApplyTransaction(env.config, bc, &coinbase, gp, env.State, env.header, tx, &env.header.GasUsed, vm.Config{})
	if err != nil {
		log.Info("file work", "func commitTransaction", err)
		env.State.RevertToSnapshot(snap)
		return err, nil
	}
	env.txs = append(env.txs, tx)
	env.Receipts = append(env.Receipts, receipt)
	mapcoingasUse.setCoinGasUse(tx, receipt.GasUsed)
	return nil, receipt.Logs
}
func (env *Work) s_commitTransaction(tx types.SelfTransaction, bc *core.BlockChain, coinbase common.Address, gp *core.GasPool) (error, []*types.Log) {
	env.State.Prepare(tx.Hash(), common.Hash{}, env.tcount)
	snap := env.State.Snapshot()
	receipt, _, err := core.ApplyTransaction(env.config, bc, &coinbase, gp, env.State, env.header, tx, &env.header.GasUsed, vm.Config{})
	if err != nil {
		log.Info("file work", "func s_commitTransaction", err)
		env.State.RevertToSnapshot(snap)
		return err, nil
	}
	tmps := make([]types.SelfTransaction, 0)
	tmps = append(tmps, tx)
	tmps = append(tmps, env.txs...)
	env.txs = tmps

	tmpr := make([]*types.Receipt, 0)
	tmpr = append(tmpr, receipt)
	tmpr = append(tmpr, env.Receipts...)
	env.Receipts = tmpr
	env.tcount++
	return nil, receipt.Logs
}

//Leader
var lostCnt int = 0

type retStruct struct {
	no  []uint32
	txs []*types.Transaction
}

func (env *Work) Reverse(s []common.RewarTx) []common.RewarTx {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}
func (env *Work) ProcessTransactions(mux *event.TypeMux, tp *core.TxPoolManager, bc *core.BlockChain) (listret []*common.RetCallTxN, retTxs []types.SelfTransaction) {
	pending, err := tp.Pending()
	if err != nil {
		log.Error("Failed to fetch pending transactions", "err", err)
		return nil, nil
	}
	mapcoingasUse.clearmap()
	tim := env.header.Time.Uint64()
	env.State.UpdateTxForBtree(uint32(tim))
	env.State.UpdateTxForBtreeBytime(uint32(tim))
	listTx := make(types.SelfTransactions, 0)
	for _, txser := range pending {
		listTx = append(listTx, txser...)
	}
	listret, retTxs = env.commitTransactions(mux, listTx, bc, common.Address{})
	tmps := make([]types.SelfTransaction, 0)
	rewart := env.CalcRewardAndSlash(bc)
	txers := env.makeTransaction(rewart)
	for _, tx := range txers {
		err, _ := env.s_commitTransaction(tx, bc, common.Address{}, new(core.GasPool).AddGas(0))
		if err != nil {
			log.Error("file work", "func ProcessTransactions:::reward Tx call Error", err)
			continue
		}
		tmptxs := make([]types.SelfTransaction, 0)
		tmptxs = append(tmptxs, tx)
		tmptxs = append(tmptxs, tmps...)
		tmps = tmptxs
	}
	tmps = append(tmps, retTxs...)
	retTxs = tmps
	return
}

func (env *Work) makeTransaction(rewarts []common.RewarTx) (txers []types.SelfTransaction) {
	for _, rewart := range rewarts {
		sorted_keys := make([]string, 0)
		for k, _ := range rewart.To_Amont {
			sorted_keys = append(sorted_keys, k.String())
		}
		sort.Strings(sorted_keys)
		extra := make([]*types.ExtraTo_tr, 0)
		var to common.Address
		var value *big.Int
		databytes := make([]byte, 0)
		isfirst := true
		for _, addr := range sorted_keys {
			k := common.HexToAddress(addr)
			v := rewart.To_Amont[k]
			if isfirst {
				if rewart.RewardTyp == common.RewardInerestType {
					databytes = append(databytes, depositAbi.Methods["interestAdd"].Id()...)
					tmpbytes, _ := depositAbi.Methods["interestAdd"].Inputs.Pack(k)
					databytes = append(databytes, tmpbytes...)
				}
				to = k
				value = v
				isfirst = false
				continue
			}
			tmp := new(types.ExtraTo_tr)
			vv := new(big.Int).Set(v)
			var kk common.Address = k
			tmp.To_tr = &kk
			tmp.Value_tr = (*hexutil.Big)(vv)
			if rewart.RewardTyp == common.RewardInerestType {
				bytes := make([]byte, 0)
				bytes = append(bytes, depositAbi.Methods["interestAdd"].Id()...)
				tmpbytes, _ := depositAbi.Methods["interestAdd"].Inputs.Pack(k)
				bytes = append(bytes, tmpbytes...)
				b := hexutil.Bytes(bytes)
				tmp.Input_tr = &b
			}
			extra = append(extra, tmp)
		}
		tx := types.NewTransactions(env.State.GetNonce(rewart.Fromaddr), to, value, 0, new(big.Int), databytes, extra, 0, common.ExtraUnGasTxType, 0)
		tx.SetFromLoad(rewart.Fromaddr)
		tx.SetTxS(big.NewInt(1))
		tx.SetTxV(big.NewInt(1))
		tx.SetTxR(big.NewInt(1))
		tx.SetTxCurrency(rewart.CoinType)
		txers = append(txers, tx)
	}

	return
}

//Broadcast
func (env *Work) ProcessBroadcastTransactions(mux *event.TypeMux, txs []types.SelfTransaction, bc *core.BlockChain) {
	tim := env.header.Time.Uint64()
	env.State.UpdateTxForBtree(uint32(tim))
	env.State.UpdateTxForBtreeBytime(uint32(tim))
	mapcoingasUse.clearmap()
	for _, tx := range txs {
		env.commitTransaction(tx, bc, common.Address{}, nil)
	}
	rewart := env.CalcRewardAndSlash(bc)
	txers := env.makeTransaction(rewart)
	for _, tx := range txers {
		err, _ := env.s_commitTransaction(tx, bc, common.Address{}, new(core.GasPool).AddGas(0))
		if err != nil {
			log.Error("file work", "func ProcessTransactions:::reward Tx call Error", err)
		}
	}
	return
}

func (env *Work) ConsensusTransactions(mux *event.TypeMux, txs []types.SelfTransaction, bc *core.BlockChain, rewardFlag bool) error {
	if env.gasPool == nil {
		env.gasPool = new(core.GasPool).AddGas(env.header.GasLimit)
	}
	mapcoingasUse.clearmap()
	var coalescedLogs []*types.Log
	tim := env.header.Time.Uint64()
	env.State.UpdateTxForBtree(uint32(tim))
	env.State.UpdateTxForBtreeBytime(uint32(tim))
	for _, tx := range txs {
		// If we don't have enough gas for any further transactions then we're done
		if env.gasPool.Gas() < params.TxGas {
			log.Trace("Not enough gas for further transactions", "have", env.gasPool, "want", params.TxGas)
			return errors.New("Not enough gas for further transactions")
		}

		// Start executing the transaction
		env.State.Prepare(tx.Hash(), common.Hash{}, env.tcount)
		err, logs := env.commitTransaction(tx, bc, common.Address{}, env.gasPool)
		if err == nil {
			env.tcount++
			coalescedLogs = append(coalescedLogs, logs...)
		} else {
			return err
		}
	}
	var rewart []common.RewarTx
	if rewardFlag {
		rewart = env.CalcRewardAndSlash(bc)
	}

	txers := env.makeTransaction(rewart)
	for _, tx := range txers {
		err, _ := env.s_commitTransaction(tx, bc, common.Address{}, new(core.GasPool).AddGas(0))
		if err != nil {
			return err
		}
	}
	if len(coalescedLogs) > 0 || env.tcount > 0 {
		// make a copy, the state caches the logs and these logs get "upgraded" from pending to mined
		// logs by filling in the block hash when the block was mined by the local miner. This can
		// cause a race condition if a log was "upgraded" before the PendingLogsEvent is processed.
		cpy := make([]*types.Log, len(coalescedLogs))
		for i, l := range coalescedLogs {
			cpy[i] = new(types.Log)
			*cpy[i] = *l
		}
		go func(logs []*types.Log, tcount int) {
			if len(logs) > 0 {
				mux.Post(core.PendingLogsEvent{Logs: logs})
			}
			if tcount > 0 {
				mux.Post(core.PendingStateEvent{})
			}
		}(cpy, env.tcount)
	}

	return nil
}
func (env *Work) GetTxs() []types.SelfTransaction {
	return env.txs
}

type randSeed struct {
	bc *core.BlockChain
}

func (r *randSeed) GetSeed(num uint64) *big.Int {
	parent := r.bc.GetBlockByNumber(num - 1)
	if parent == nil {
		log.Error(packagename, "获取父区块错误,高度", (num - 1))
		return big.NewInt(0)
	}
	//_, preVrfValue, _ := common.GetVrfInfoFromHeader(parent.Header().VrfValue)
	//seed := common.BytesToHash(preVrfValue).Big()
	seed:=big.NewInt(0)
	return seed
}

func (env *Work) CalcRewardAndSlash(bc *core.BlockChain) []common.RewarTx {
	bcInterval, err := manparams.NewBCIntervalByHash(env.header.ParentHash)
	if err != nil {
		log.Error("work", "获取广播周期失败", err)
		return nil
	}
	if bcInterval.IsBroadcastNumber(env.header.Number.Uint64()) {
		return nil
	}
	blkReward := blkreward.New(bc, env.State)
	rewardList := make([]common.RewarTx, 0)
	if nil != blkReward {
		//todo: read half number from state
		minersRewardMap := blkReward.CalcMinerRewards(env.header.Number.Uint64())
		if 0 != len(minersRewardMap) {
			rewardList = append(rewardList, common.RewarTx{CoinType: "MAN", Fromaddr: common.BlkMinerRewardAddress, To_Amont: minersRewardMap})
		}

		validatorsRewardMap := blkReward.CalcValidatorRewards(env.header.Leader, env.header.Number.Uint64())
		if 0 != len(validatorsRewardMap) {
			rewardList = append(rewardList, common.RewarTx{CoinType: "MAN", Fromaddr: common.BlkValidatorRewardAddress, To_Amont: validatorsRewardMap})
		}
	}

	allGas := env.getGas()
	txsReward := txsreward.New(bc, env.State)
	if nil != txsReward {
		txsRewardMap := txsReward.CalcNodesRewards(allGas, env.header.Leader, env.header.Number.Uint64())
		if 0 != len(txsRewardMap) {
			rewardList = append(rewardList, common.RewarTx{CoinType: "MAN", Fromaddr: common.TxGasRewardAddress, To_Amont: txsRewardMap})
		}
	}
	lottery := lottery.New(bc, env.State, env.random)
	if nil != lottery {
		lotteryRewardMap := lottery.LotteryCalc(env.header.ParentHash, env.header.Number.Uint64())
		if 0 != len(lotteryRewardMap) {
			rewardList = append(rewardList, common.RewarTx{CoinType: "MAN", Fromaddr: common.LotteryRewardAddress, To_Amont: lotteryRewardMap})
		}
	}

	////todo 利息
	interestReward := interest.New(env.State)
	if nil != interestReward {
		interestRewardMap := interestReward.InterestCalc(env.State, env.header.Number.Uint64())
		if 0 != len(interestRewardMap) {
			rewardList = append(rewardList, common.RewarTx{CoinType: "MAN", Fromaddr: common.InterestRewardAddress, To_Amont: interestRewardMap, RewardTyp: common.RewardInerestType})
		}
	}
	//todo 惩罚

	slash := slash.New(bc, env.State)
	if nil != slash {
		slash.CalcSlash(env.State, env.header.Number.Uint64(), env.uptime)
	}

	return env.Reverse(rewardList)
}

func (env *Work) getGas() *big.Int {

	price := mapcoingasUse.getCoinGasPrice("MAN")
	gas := mapcoingasUse.getCoinGasUse("MAN")
	allGas := new(big.Int).Mul(gas, price)
	log.INFO("奖励", "交易费奖励总额", allGas.String())
	balance := env.State.GetBalance(common.TxGasRewardAddress)

	if len(balance) == 0 {
		log.WARN("奖励", "交易费奖励账户余额不合法", "")
		return big.NewInt(0)
	}

	if balance[common.MainAccount].Balance.Cmp(big.NewInt(0)) <= 0 || balance[common.MainAccount].Balance.Cmp(allGas) <= 0 {
		log.WARN("奖励", "交易费奖励账户余额不合法，余额", balance)
		return big.NewInt(0)
	}
	return allGas
}
func (env *Work) GetUpTimeAccounts(num uint64, bc *core.BlockChain, bcInterval *manparams.BCInterval) ([]common.Address, error) {
	originData, err := bc.GetMatrixStateDataByNumber(mc.MSKeyElectGenTime, num-1)
	if err != nil {
		log.ERROR("blockchain", "获取选举生成点配置失败 err", err)
		return nil, err
	}
	electGenConf, Ok := originData.(*mc.ElectGenTimeStruct)
	if Ok == false {
		log.ERROR("blockchain", "选举生成点信息失败 err", err)
		return nil, err
	}

	log.INFO(packagename, "获取所有参与uptime点名高度", num)

	upTimeAccounts := make([]common.Address, 0)

	minerNum := num - (num % bcInterval.GetBroadcastInterval()) - uint64(electGenConf.MinerGen)
	log.INFO(packagename, "参选矿工节点uptime高度", minerNum)
	ans, err := ca.GetElectedByHeightAndRole(big.NewInt(int64(minerNum)), common.RoleMiner)
	if err != nil {
		return nil, err
	}

	for _, v := range ans {
		upTimeAccounts = append(upTimeAccounts, v.Address)
		log.INFO("packagename", "矿工节点账户", v.Address.Hex())
	}
	validatorNum := num - (num % bcInterval.GetBroadcastInterval()) - uint64(electGenConf.ValidatorGen)
	log.INFO(packagename, "参选验证节点uptime高度", validatorNum)
	ans1, err := ca.GetElectedByHeightAndRole(big.NewInt(int64(validatorNum)), common.RoleValidator)
	if err != nil {
		return upTimeAccounts, err
	}
	for _, v := range ans1 {
		upTimeAccounts = append(upTimeAccounts, v.Address)
		log.INFO("packagename", "验证者节点账户", v.Address.Hex())
	}
	return upTimeAccounts, nil
}

func (env *Work) GetUpTimeData(hash common.Hash) (map[common.Address]uint32, map[common.Address][]byte, error) {

	log.INFO(packagename, "获取所有心跳交易", "")
	//%99
	heatBeatUnmarshallMMap, error := core.GetBroadcastTxs(hash, mc.Heartbeat)
	if nil != error {
		log.WARN(packagename, "获取主动心跳交易错误", error)
	}
	//每个广播周期发一次
	calltherollUnmarshall, error := core.GetBroadcastTxs(hash, mc.CallTheRoll)
	if nil != error {
		log.ERROR(packagename, "获取点名心跳交易错误", error)
		return nil, nil, error
	}
	calltherollMap := make(map[common.Address]uint32, 0)
	for _, v := range calltherollUnmarshall {
		temp := make(map[string]uint32, 0)
		error := json.Unmarshal(v, &temp)
		if nil != error {
			log.ERROR(packagename, "序列化点名心跳交易错误", error)
			return nil, nil, error
		}
		log.INFO(packagename, "++++++++点名心跳交易++++++++", temp)
		for k, v := range temp {
			calltherollMap[common.HexToAddress(k)] = v
		}
	}
	return calltherollMap, heatBeatUnmarshallMMap, nil
}

func (env *Work) HandleUpTime(state *state.StateDB, accounts []common.Address, calltherollRspAccounts map[common.Address]uint32, heatBeatAccounts map[common.Address][]byte, blockNum uint64, bc *core.BlockChain, bcInterval *manparams.BCInterval) error {
	var blockHash common.Hash
	HeatBeatReqAccounts := make([]common.Address, 0)
	HeartBeatMap := make(map[common.Address]bool, 0)
	broadcastInterval := bcInterval.GetBroadcastInterval()
	blockNumRem := blockNum % broadcastInterval

	//subVal就是最新的广播区块，例如当前区块高度是198或者是101，那么subVal就是100
	subVal := blockNum - blockNumRem
	//subVal就是最新的广播区块，例如当前区块高度是198或者是101，那么subVal就是100
	subVal = subVal
	if blockNum < broadcastInterval { //当前区块小于100说明是100区块内 (下面的if else是为了应对中途加入的参选节点)
		blockHash = bc.GetBlockByNumber(0).Hash() //创世区块的hash
	} else {
		blockHash = bc.GetBlockByNumber(subVal).Hash() //获取最近的广播区块的hash
	}
	// todo: remove
	//blockHash = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3e4")
	broadcastBlock := blockHash.Big()
	val := broadcastBlock.Uint64() % (broadcastInterval - 1)

	for _, v := range accounts {
		currentAcc := v.Big() //YY TODO 这里应该是广播账户。后期需要修改
		ret := currentAcc.Uint64() % (broadcastInterval - 1)
		if ret == val {
			HeatBeatReqAccounts = append(HeatBeatReqAccounts, v)
			if _, ok := heatBeatAccounts[v]; ok {
				HeartBeatMap[v] = true
			} else {
				HeartBeatMap[v] = false

			}
			log.Info(packagename, "计算主动心跳的账户", v, "心跳状态", HeartBeatMap[v])
		}
	}

	var upTime uint64
	originTopologyNum := blockNum - blockNum%broadcastInterval - 1
	log.Info(packagename, "获取原始拓扑图所有的验证者和矿工，高度为", originTopologyNum)
	originTopology, err := ca.GetTopologyByNumber(common.RoleValidator|common.RoleBackupValidator|common.RoleMiner|common.RoleBackupMiner, originTopologyNum)
	if err != nil {
		return err
	}
	originTopologyMap := make(map[common.Address]uint32, 0)
	for _, v := range originTopology.NodeList {
		originTopologyMap[v.Account] = 0
	}
	for _, account := range accounts {
		onlineBlockNum, ok := calltherollRspAccounts[account]
		if ok { //被点名,使用点名的uptime
			upTime = uint64(onlineBlockNum)
			log.INFO(packagename, "点名账号", account, "uptime", upTime)

		} else { //没被点名，没有主动上报，则为最大值，
			if v, ok := HeartBeatMap[account]; ok { //有主动上报
				if v {
					upTime = broadcastInterval - 3
					log.INFO(packagename, "没被点名，有主动上报有响应", account, "uptime", upTime)
				} else {
					upTime = 0
					log.INFO(packagename, "没被点名，有主动上报无响应", account, "uptime", upTime)
				}
			} else { //没被点名和主动上报
				upTime = broadcastInterval - 3
				log.INFO(packagename, "没被点名，没要求主动上报", account, "uptime", upTime)

			}
		}
		// todo: add
		depoistInfo.AddOnlineTime(state, account, new(big.Int).SetUint64(upTime))
		read, err := depoistInfo.GetOnlineTime(state, account)
		env.uptime[account] = upTime
		if nil == err {
			log.INFO(packagename, "读取状态树", account, "upTime减半", read)
			if _, ok := originTopologyMap[account]; ok {
				updateData := new(big.Int).SetUint64(read.Uint64() / 2)
				log.INFO(packagename, "是原始拓扑图节点，upTime减半", account, "upTime", updateData.Uint64())
				depoistInfo.AddOnlineTime(state, account, updateData)
			}
		}

	}

	return nil
}

func (env *Work) HandleUpTimeWithSuperBlock(state *state.StateDB, accounts []common.Address, blockNum uint64, bcInterval *manparams.BCInterval) error {
	broadcastInterval := bcInterval.GetBroadcastInterval()
	originTopologyNum := blockNum - blockNum%broadcastInterval - 1
	originTopology, err := ca.GetTopologyByNumber(common.RoleValidator|common.RoleBackupValidator|common.RoleMiner|common.RoleBackupMiner, originTopologyNum)
	if err != nil {
		return err
	}
	originTopologyMap := make(map[common.Address]uint32, 0)
	for _, v := range originTopology.NodeList {
		originTopologyMap[v.Account] = 0
	}
	for _, account := range accounts {

		upTime := broadcastInterval - 3
		log.INFO(packagename, "没被点名，没要求主动上报", account, "uptime", upTime)

		// todo: add
		depoistInfo.AddOnlineTime(state, account, new(big.Int).SetUint64(upTime))
		read, err := depoistInfo.GetOnlineTime(state, account)
		env.uptime[account] = upTime
		if nil == err {
			log.INFO(packagename, "读取状态树", account, "upTime减半", read)
			if _, ok := originTopologyMap[account]; ok {
				updateData := new(big.Int).SetUint64(read.Uint64() / 2)
				log.INFO(packagename, "是原始拓扑图节点，upTime减半", account, "upTime", updateData.Uint64())
				depoistInfo.AddOnlineTime(state, account, updateData)
			}
		}

	}
	return nil

}
