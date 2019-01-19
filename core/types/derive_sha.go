// Copyright (c) 2018 The MATRIX Authors
// Distributed under the MIT software license, see the accompanying
// file COPYING or or http://www.opensource.org/licenses/mit-license.php

package types

import (
	"bytes"

	"github.com/matrix/go-matrix/common"
	"github.com/matrix/go-matrix/log"
	"github.com/matrix/go-matrix/rlp"
	"github.com/matrix/go-matrix/trie"
)

type DerivableList interface {
	Len() int
	GetRlp(i int) []byte
}

func DeriveSha(list DerivableList) common.Hash {
	keybuf := new(bytes.Buffer)
	trie := new(trie.Trie)
	//log.Info("DeriveSha Empty Hash", "hash", trie.Hash())
	//	log.Info("DeriveSha Trie Root Type", "Type Name",trie.Root())
	for i := 0; i < list.Len(); i++ {
		keybuf.Reset()
		rlp.Encode(keybuf, uint(i))
		trie.Update(keybuf.Bytes(), list.GetRlp(i))
	}
	log.Info("DeriveSha Result Hash", "hash", trie.Hash())
	return trie.Hash()
}
func DeriveShaHash(list []common.Hash) common.Hash {
	keybuf := new(bytes.Buffer)
	trie := new(trie.Trie)
	log.Info("DeriveSha Empty Hash", "hash", trie.Hash())
	//	log.Info("DeriveSha Trie Root Type", "Type Name",trie.Root())
	for i := 0; i < len(list); i++ {
		keybuf.Reset()
		rlp.Encode(keybuf, uint(i))
		trie.Update(keybuf.Bytes(), list[i][:])
	}
	log.Info("DeriveSha Result Hash", "hash", trie.Hash())
	return trie.Hash()
}
