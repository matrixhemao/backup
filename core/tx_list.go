// Copyright (c) 2018 The MATRIX Authors
// Distributed under the MIT software license, see the accompanying
// file COPYING or or http://www.opensource.org/licenses/mit-license.php

package core

import (
	"container/heap"
	"math"
	"math/big"
	"sort"

	"github.com/MatrixAINetwork/go-matrix/core/types"
)

// nonceHeap is a heap.Interface implementation over 64bit unsigned integers for
// retrieving sorted transactions from the possibly gapped future queue.
type nonceHeap []uint64

func (h nonceHeap) Len() int           { return len(h) }
func (h nonceHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h nonceHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *nonceHeap) Push(x interface{}) {
	*h = append(*h, x.(uint64))
}

func (h *nonceHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// txSortedMap is a nonce->transaction hash map with a heap based index to allow
// iterating over the contents in a nonce-incrementing way.
type txSortedMap struct {
	items map[uint64]*types.Transaction // Hash map storing the transaction data
	index *nonceHeap                    // Heap of nonces of all the stored transactions (non-strict mode)
	cache []*types.Transaction          // Cache of the transactions already sorted
	//cache []types.SelfTransaction
}

// newTxSortedMap creates a new nonce-sorted transaction map.
func newTxSortedMap() *txSortedMap {
	return &txSortedMap{
		items: make(map[uint64]*types.Transaction),
		index: new(nonceHeap),
	}
}

// Get retrieves the current transactions associated with the given nonce.
func (m *txSortedMap) Get(nonce uint64) *types.Transaction {
	return m.items[nonce]
}

// Put inserts a new transaction into the map, also updating the map's nonce
// index. If a transaction already exists with the same nonce, it's overwritten.
func (m *txSortedMap) Put(tx *types.Transaction) {
	nonce := tx.Nonce()
	if m.items[nonce] == nil {
		heap.Push(m.index, nonce)
	}
	m.items[nonce], m.cache = tx, nil
}

// Forward removes all transactions from the map with a nonce lower than the
// provided threshold. Every removed transaction is returned for any post-removal
// maintenance.
func (m *txSortedMap) Forward(threshold uint64) []*types.Transaction {
	var removed []*types.Transaction

	// Pop off heap items until the threshold is reached
	for m.index.Len() > 0 && (*m.index)[0] < threshold {
		nonce := heap.Pop(m.index).(uint64)
		removed = append(removed, m.items[nonce])
		delete(m.items, nonce)
	}
	// If we had a cached order, shift the front
	if m.cache != nil {
		m.cache = m.cache[len(removed):]
	}
	return removed
}

// Filter iterates over the list of transactions and removes all of them for which
// the specified function evaluates to true.
func (m *txSortedMap) Filter(filter func(*types.Transaction) bool) []*types.Transaction {
	var removed []*types.Transaction

	// Collect all the transactions to filter out
	for nonce, tx := range m.items {
		if filter(tx) {
			removed = append(removed, tx)
			delete(m.items, nonce)
		}
	}
	// If transactions were removed, the heap and cache are ruined
	if len(removed) > 0 {
		*m.index = make([]uint64, 0, len(m.items))
		for nonce := range m.items {
			*m.index = append(*m.index, nonce)
		}
		heap.Init(m.index)

		m.cache = nil
	}
	return removed
}

// Cap places a hard limit on the number of items, returning all transactions
// exceeding that limit.
func (m *txSortedMap) Cap(threshold int) []*types.Transaction {
	// Short circuit if the number of items is under the limit
	if len(m.items) <= threshold {
		return nil
	}
	// Otherwise gather and drop the highest nonce'd transactions
	var drops []*types.Transaction

	sort.Sort(*m.index)
	for size := len(m.items); size > threshold; size-- {
		drops = append(drops, m.items[(*m.index)[size-1]])
		delete(m.items, (*m.index)[size-1])
	}
	*m.index = (*m.index)[:threshold]
	heap.Init(m.index)

	// If we had a cache, shift the back
	if m.cache != nil {
		m.cache = m.cache[:len(m.cache)-len(drops)]
	}
	return drops
}

// Remove deletes a transaction from the maintained map, returning whether the
// transaction was found.
func (m *txSortedMap) Remove(nonce uint64) bool {
	// Short circuit if no transaction is present
	_, ok := m.items[nonce]
	if !ok {
		return false
	}
	// Otherwise delete the transaction and fix the heap index
	for i := 0; i < m.index.Len(); i++ {
		if (*m.index)[i] == nonce {
			heap.Remove(m.index, i)
			break
		}
	}
	delete(m.items, nonce)
	m.cache = nil

	return true
}

// Ready retrieves a sequentially increasing list of transactions starting at the
// provided nonce that is ready for processing. The returned transactions will be
// removed from the list.
//
// Note, all transactions with nonces lower than start will also be returned to
// prevent getting into and invalid state. This is not something that should ever
// happen but better to be self correcting than failing!
//func (m *txSortedMap) Ready(start uint64) []*types.Transaction {
//	// Short circuit if no transactions are available
//	//if m.index.Len() == 0 || (*m.index)[0] > start {
//	//	return nil
//	//}
//
//	//  ;交易洪泛时不用判断nonce连续
//	if m.index.Len() == 0 {
//		return nil
//	}
//
//	// Otherwise start accumulating incremental transactions
//	var ready []*types.Transaction
//	for next := (*m.index)[0]; m.index.Len() > 0; /*&& (*m.index)[0] == next*/ next++ {
//		if (*m.index)[0] == next {
//			ready = append(ready, m.items[next])
//			delete(m.items, next)
//			heap.Pop(m.index)
//		} else { // 给上面代码添加if 以及添加else
//			n := (*m.index)[0]
//			ready = append(ready, m.items[n])
//			delete(m.items, n)
//			heap.Pop(m.index)
//		}
//
//	}
//	m.cache = nil
//
//	return ready
//}

// Len returns the length of the transaction map.
func (m *txSortedMap) Len() int {
	return len(m.items)
}

// Flatten creates a nonce-sorted slice of transactions based on the loosely
// sorted internal representation. The result of the sorting is cached in case
// it's requested again before any modifications are made to the contents.
func (m *txSortedMap) Flatten() []*types.Transaction {
	// If the sorting was not cached yet, create and cache it
	if m.cache == nil {
		m.cache = make([]*types.Transaction, 0, len(m.items))
		tmptxser := make([]types.SelfTransaction, 0)
		for _, tx := range m.items {
			//m.cache = append(m.cache, tx)
			tmptxser = append(tmptxser, tx)
		}
		sort.Sort(types.TxByNonce(tmptxser))
		for _, txer := range tmptxser {
			tx, ok := txer.(*types.Transaction)
			if ok {
				m.cache = append(m.cache, tx)
			}
		}

	}
	// Copy the cache to prevent accidental modifications
	txs := make([]*types.Transaction, len(m.cache))
	copy(txs, m.cache)
	return txs
}

// txList is a "list" of transactions belonging to an account, sorted by account
// nonce. The same type can be used both for storing contiguous transactions for
// the executable/pending queue; and for storing gapped transactions for the non-
// executable/future queue, with minor behavioral changes.
type txList struct {
	strict bool                    // Whether nonces are strictly continuous or not
	txs    map[string]*txSortedMap // Heap indexed sorted hash map of the transactions

	costcap *big.Int // Price of the highest costing transaction (reset only if exceeds balance)
	gascap  uint64   // Gas limit of the highest spending transaction (reset only if exceeds block limit)
}

// newTxList create a new transaction list for maintaining nonce-indexable fast,
// gapped, sortable transaction lists.
func newTxList(strict bool, str string) *txList {
	sortmap := newTxSortedMap()
	tl := &txList{
		strict:  strict,
		txs:     make(map[string]*txSortedMap),
		costcap: new(big.Int),
	}
	if str == "" {
		str = "MAN"
	}
	tl.txs[str] = sortmap //主币种
	return tl

}

// Overlaps returns whether the transaction specified has the same nonce as one
// already contained within the list.
func (l *txList) Overlaps(tx *types.Transaction) bool {
	sm, ok := l.txs[tx.GetTxCurrency()]
	if !ok {
		return false
	}
	return sm.Get(tx.Nonce()) != nil
	//return l.txs.Get(tx.Nonce()) != nil
}

// Add tries to insert a new transaction into the list, returning whether the
// transaction was accepted, and if yes, any previous transaction it replaced.
//
// If the new transaction is accepted into the list, the lists' cost and gas
// thresholds are also potentially updated.
func (l *txList) Add(tx *types.Transaction, priceBump uint64) (bool, *types.Transaction) {
	//// If there's an older better transaction, abort
	//old := l.txs.Get(tx.Nonce())
	//if old != nil {
	//	threshold := new(big.Int).Div(new(big.Int).Mul(old.GasPrice(), big.NewInt(100+int64(priceBump))), big.NewInt(100))
	//	// Have to ensure that the new gas price is higher than the old gas
	//	// price as well as checking the percentage threshold to ensure that
	//	// this is accurate for low (Wei-level) gas price replacements
	//	if old.GasPrice().Cmp(tx.GasPrice()) >= 0 || threshold.Cmp(tx.GasPrice()) > 0 {
	//		return false, nil
	//	}
	//}
	// Otherwise overwrite the old transaction with the current one
	sm, ok := l.txs[tx.GetTxCurrency()]
	if !ok {
		l.txs[tx.GetTxCurrency()] = newTxSortedMap()
		sm = l.txs[tx.GetTxCurrency()]
	}
	sm.Put(tx)
	if cost := tx.Cost(); l.costcap.Cmp(cost) < 0 {
		l.costcap = cost
	}
	if gas := tx.Gas(); l.gascap < gas {
		l.gascap = gas
	}
	return true, nil
}

// Forward removes all transactions from the list with a nonce lower than the
// provided threshold. Every removed transaction is returned for any post-removal
// maintenance.
//func (l *txList) Forward(threshold uint64) []*types.Transaction {
//	return l.txs.Forward(threshold)
//}

// Filter removes all transactions from the list with a cost or gas limit higher
// than the provided thresholds. Every removed transaction is returned for any
// post-removal maintenance. Strict-mode invalidated transactions are also
// returned.
//
// This method uses the cached costcap and gascap to quickly decide if there's even
// a point in calculating all the costs or if the balance covers all. If the threshold
// is lower than the costgas cap, the caps will be reset to a new high after removing
// the newly invalidated transactions.
func (l *txList) Filter(costLimit *big.Int, gasLimit uint64, typ string) ([]*types.Transaction, []*types.Transaction) {
	// If all transactions are below the threshold, short circuit
	if l.costcap.Cmp(costLimit) <= 0 && l.gascap <= gasLimit {
		return nil, nil
	}
	l.costcap = new(big.Int).Set(costLimit) // Lower the caps to the thresholds
	l.gascap = gasLimit

	sm, ok := l.txs[typ]
	if !ok {
		return nil, nil
	}
	// Filter out all the transactions above the account's funds
	removed := sm.Filter(func(tx *types.Transaction) bool { return tx.Cost().Cmp(costLimit) > 0 || tx.Gas() > gasLimit })

	// If the list was strict, filter anything above the lowest nonce
	var invalids []*types.Transaction

	if l.strict && len(removed) > 0 {
		lowest := uint64(math.MaxUint64)
		for _, tx := range removed {
			if nonce := tx.Nonce(); lowest > nonce {
				lowest = nonce
			}
		}
		invalids = sm.Filter(func(tx *types.Transaction) bool { return tx.Nonce() > lowest })
	}
	return removed, invalids
}

// Cap places a hard limit on the number of items, returning all transactions
// exceeding that limit.
//func (l *txList) Cap(threshold int) []*types.Transaction {
//	return l.txs.Cap(threshold)
//}

// Remove deletes a transaction from the maintained list, returning whether the
// transaction was found, and also returning any transaction invalidated due to
// the deletion (strict mode only).
func (l *txList) Remove(tx *types.Transaction) (bool, []*types.Transaction) {
	// Remove the transaction from the set
	nonce := tx.Nonce()
	sm, ok := l.txs[tx.GetTxCurrency()]
	if !ok {
		return false, nil
	}
	if removed := sm.Remove(nonce); !removed {
		return false, nil
	}
	// In strict mode, filter out non-executable transactions
	if l.strict {
		return true, sm.Filter(func(tx *types.Transaction) bool { return tx.Nonce() > nonce })
	}
	return true, nil
}

// Ready retrieves a sequentially increasing list of transactions starting at the
// provided nonce that is ready for processing. The returned transactions will be
// removed from the list.
//
// Note, all transactions with nonces lower than start will also be returned to
// prevent getting into and invalid state. This is not something that should ever
// happen but better to be self correcting than failing!
//func (l *txList) Ready(start uint64) []*types.Transaction {
//	return l.txs.Ready(start)
//}

// Len returns the length of the transaction list.
func (l *txList) Len(typ string) int {
	sm, ok := l.txs[typ]
	if !ok {
		return 0
	}
	return sm.Len()
}

// Empty returns whether the list of transactions is empty or not.
func (l *txList) Empty(typ string) bool {
	return l.Len(typ) == 0
}

// Flatten creates a nonce-sorted slice of transactions based on the loosely
// sorted internal representation. The result of the sorting is cached in case
// it's requested again before any modifications are made to the contents.
//func (l *txList) Flatten() []*types.Transaction {
//	return l.txs.Flatten()
//}

/*
// priceHeap is a heap.Interface implementation over transactions for retrieving
// price-sorted transactions to discard when the pool fills up.
type priceHeap []*types.Transaction

func (h priceHeap) Len() int      { return len(h) }
func (h priceHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h priceHeap) Less(i, j int) bool {
	// Sort primarily by price, returning the cheaper one
	switch h[i].GasPrice().Cmp(h[j].GasPrice()) {
	case -1:
		return true
	case 1:
		return false
	}
	// If the prices match, stabilize via nonces (high nonce is worse)
	return h[i].Nonce() > h[j].Nonce()
}

func (h *priceHeap) Push(x interface{}) {
	*h = append(*h, x.(*types.Transaction))
}

func (h *priceHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// txPricedList is a price-sorted heap to allow operating on transactions pool
// contents in a price-incrementing way.
type txPricedList struct {
	all    *txLookup  // Pointer to the map of all transactions
	items  *priceHeap // Heap of prices of all the stored transactions
	stales int        // Number of stale price points to (re-heap trigger)
}

// newTxPricedList creates a new price-sorted transaction heap.
func newTxPricedList(all *txLookup) *txPricedList {
	return &txPricedList{
		all:   all,
		items: new(priceHeap),
	}
}

// Put inserts a new transaction into the heap.
func (l *txPricedList) Put(tx *types.Transaction) {
	heap.Push(l.items, tx)
}

// Removed notifies the prices transaction list that an old transaction dropped
// from the pool. The list will just keep a counter of stale objects and update
// the heap if a large enough ratio of transactions go stale.
func (l *txPricedList) Removed() {
	// Bump the stale counter, but exit if still too low (< 25%)
	l.stales++
	if l.stales <= len(*l.items)/4 {
		return
	}
	// Seems we've reached a critical number of stale transactions, reheap
	reheap := make(priceHeap, 0, l.all.Count())

	l.stales, l.items = 0, &reheap
	l.all.Range(func(hash common.Hash, tx *types.Transaction) bool {
		*l.items = append(*l.items, tx)
		return true
	})
	heap.Init(l.items)
}

// Cap finds all the transactions below the given price threshold, drops them
// from the priced list and returs them for further removal from the entire pool.
func (l *txPricedList) Cap(threshold *big.Int, local *accountSet) types.Transactions {
	drop := make(types.Transactions, 0, 128) // Remote underpriced transactions to drop
	save := make(types.Transactions, 0, 64)  // Local underpriced transactions to keep

	for len(*l.items) > 0 {
		// Discard stale transactions if found during cleanup
		tx := heap.Pop(l.items).(*types.Transaction)
		if l.all.Get(tx.Hash()) == nil {
			l.stales--
			continue
		}
		// Stop the discards if we've reached the threshold
		if tx.GasPrice().Cmp(threshold) >= 0 {
			save = append(save, tx)
			break
		}
		// Non stale transaction found, discard unless local
		if local.containsTx(tx) {
			save = append(save, tx)
		} else {
			drop = append(drop, tx)
		}
	}
	for _, tx := range save {
		heap.Push(l.items, tx)
	}
	return drop
}

// Underpriced checks whether a transaction is cheaper than (or as cheap as) the
// lowest priced transaction currently being tracked.
func (l *txPricedList) Underpriced(tx *types.Transaction, local *accountSet) bool {
	// Local transactions cannot be underpriced
	if local.containsTx(tx) {
		return false
	}
	// Discard stale price points if found at the heap start
	for len(*l.items) > 0 {
		head := []*types.Transaction(*l.items)[0]
		if l.all.Get(head.Hash()) == nil {
			l.stales--
			heap.Pop(l.items)
			continue
		}
		break
	}
	// Check if the transaction is underpriced or not
	if len(*l.items) == 0 {
		log.Error("Pricing query for empty pool") // This cannot happen, print to catch programming errors
		return false
	}
	cheapest := []*types.Transaction(*l.items)[0]
	return cheapest.GasPrice().Cmp(tx.GasPrice()) >= 0
}

// Discard finds a number of most underpriced transactions, removes them from the
// priced list and returns them for further removal from the entire pool.
func (l *txPricedList) Discard(count int, local *accountSet) types.Transactions {
	drop := make(types.Transactions, 0, count) // Remote underpriced transactions to drop
	save := make(types.Transactions, 0, 64)    // Local underpriced transactions to keep

	for len(*l.items) > 0 && count > 0 {
		// Discard stale transactions if found during cleanup
		tx := heap.Pop(l.items).(*types.Transaction)
		if l.all.Get(tx.Hash()) == nil {
			l.stales--
			continue
		}
		// Non stale transaction found, discard unless local
		if local.containsTx(tx) {
			save = append(save, tx)
		} else {
			drop = append(drop, tx)
			count--
		}
	}
	for _, tx := range save {
		heap.Push(l.items, tx)
	}
	return drop
}

*/
