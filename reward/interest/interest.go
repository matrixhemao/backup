package interest

import (
	"github.com/matrix/go-matrix/ca"
	"github.com/matrix/go-matrix/common"
	"github.com/matrix/go-matrix/core/vm"
	"github.com/matrix/go-matrix/depoistInfo"
	"github.com/matrix/go-matrix/log"
	"math/big"
	"sort"
)
const (
	PackageName = "利息奖励"
	Denominator = 10000000
)

type interest struct {
}

type DepositInterestRate struct {
	Deposit *big.Int
	Interst *big.Rat
}

type DepositInterestRateList []*DepositInterestRate
func (p DepositInterestRateList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p DepositInterestRateList) Len() int           { return len(p) }
func (p DepositInterestRateList) Less(i, j int) bool { return p[i].Deposit.Cmp(p[j].Deposit)<0  }

func New() *interest {

	return &interest{}
}
func (tlr *interest)calcNodeInterest(deposit *big.Int,depositInterestRate []*DepositInterestRate)*big.Int{

	var blockInterest *big.Rat = nil
	for i,depositInteres:= range depositInterestRate{
		if deposit.Cmp(big.NewInt(0)) <= 0 {
			log.ERROR(PackageName, "抵押获取错误", deposit)
			return big.NewInt(0)
		}
		if deposit.Cmp(depositInteres.Deposit)<0{
			blockInterest = depositInterestRate[i-1].Interst
			break
		}
	}
	if blockInterest==nil{
		blockInterest = depositInterestRate[len(depositInterestRate)-1].Interst
	}
	interstReward,_:= new(big.Rat).Mul(new(big.Rat).SetInt(deposit), blockInterest).Float64()
	bigval := new(big.Float)
	bigval.SetFloat64(interstReward)
	result := new(big.Int)
	bigval.Int(result)
	//log.INFO(PackageName, "calc interest reward  all reward", interstReward, "reward", result.String())
	return  result
}

func (ic *interest) InterestCalc(state vm.StateDB, num uint64) {
	//todo:状态树读取利息计算的周期、支付的周期、利率
	if num<common.GetBroadcastInterval(){
		return
	}
	if nil == state {
		log.ERROR(PackageName, "状态树是空", state)
		return
	}
	calcInterestPeriod:=common.GetBroadcastInterval()
	payInterestPeriod:=3*common.GetBroadcastInterval()

	if payInterestPeriod < calcInterestPeriod {
		log.ERROR(PackageName, "配置的发放周期小于计息周期", "")
		return
	}
	depositInterestRateList := DepositInterestRateList{
		&DepositInterestRate{big.NewInt(0),big.NewRat(5,Denominator)},
		&DepositInterestRate{new(big.Int).Exp(big.NewInt(10), big.NewInt(21), big.NewInt(0)),big.NewRat(10,Denominator)},
		&DepositInterestRate{new(big.Int).Exp(big.NewInt(10), big.NewInt(22), big.NewInt(0)),big.NewRat(15,Denominator)},
		&DepositInterestRate{new(big.Int).Exp(big.NewInt(10), big.NewInt(23), big.NewInt(0)),big.NewRat(20,Denominator)},
	}
	sort.Sort(depositInterestRateList)
	//sort.Search()
	if calcInterestPeriod==0||0==payInterestPeriod{
		log.ERROR(PackageName,"InterestPeriod is  error","")
		return
	}

	if calcInterestPeriod==1||0==(num-1)%uint64(calcInterestPeriod){
		depositNodes, err := ca.GetElectedByHeight(new(big.Int).SetUint64(num - 1))
		if nil != err {
			log.ERROR(PackageName, "获取的抵押列表错误", err)
			return
		}
		if 0 == len(depositNodes) {
			log.ERROR(PackageName, "获取的抵押列表为空", "")
			return
		}
		log.INFO(PackageName, "计算利息,高度", num)
		for _, v := range depositNodes {

			result:=ic.calcNodeInterest(v.Deposit,depositInterestRateList)
			if result.Cmp(big.NewInt(0)) <= 0 {
				log.ERROR(PackageName, "计算的利息非法", result)
				continue
			}
			depoistInfo.AddInterest(state,v.Address,result)
			log.INFO(PackageName, "账户", v.Address.String(), "deposit",v.Deposit.String(),  "利息", result.String())
		}
		log.INFO(PackageName, "计算利息后", "")
	}

	if payInterestPeriod==1||0==(num-1)%uint64(payInterestPeriod) {
		//1.获取所有利息转到抵押账户 2.清除所有利息
		log.INFO(PackageName, "将利息转到合约抵押账户,高度", num)

		AllInterestMap := depoistInfo.GetAllInterest(state)
		Deposit := depoistInfo.GetDeposit(state)
		if Deposit.Cmp(big.NewInt(0)) < 0 {
			log.WARN(PackageName, "利息合约账户余额非法", Deposit)
		}

		log.INFO(PackageName, "设置利息前的合约抵押账户余额", Deposit)
		for account, interest := range AllInterestMap {
			log.INFO(PackageName,"账户",account,"利息",interest.String())
			if interest.Cmp(big.NewInt(0)) <= 0 {
				log.ERROR(PackageName, "获取的利息非法", interest)
				continue
			}
			Deposit = new(big.Int).Add(Deposit, interest)
			depoistInfo.ResetInterest(state,account)
		}
		depoistInfo.SetDeposit(state, Deposit)
		readDeposit := depoistInfo.GetDeposit(state)
		log.INFO(PackageName,"设置利息后的合约抵押账户余额",readDeposit)
	}
}