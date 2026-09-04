package shardctrler

import (
	"fmt"
	"time"

	"6.5840/kvsrv1"
	"6.5840/kvsrv1/rpc"
	"6.5840/kvtest1"
	"6.5840/shardkv1/shardcfg"
	"6.5840/shardkv1/shardgrp"
	"6.5840/tester1"
)

const (
	configKey = "config"
	nextKey   = "next"
)

type ShardCtrler struct {
	clnt *tester.Clnt
	kvtest.IKVClerk

	killed int32
}

func MakeShardCtrler(clnt *tester.Clnt) *ShardCtrler {
	sck := &ShardCtrler{clnt: clnt}
	sck.IKVClerk = kvsrv.MakeClerk(clnt, tester.ServerName(tester.GRP0, 0))
	return sck
}

func (sck *ShardCtrler) InitController() {
	next := sck.get(nextKey)
	current := sck.Query()
	if next != nil && (current == nil || next.Num > current.Num) {
		sck.apply(next)
	}
}

func (sck *ShardCtrler) InitConfig(cfg *shardcfg.ShardConfig) {
	sck.publish(cfg)
}

func (sck *ShardCtrler) get(key string) *shardcfg.ShardConfig {
	value, _, err := sck.Get(key)
	if err != rpc.OK || value == "" {
		return nil
	}
	return shardcfg.FromString(value)
}

func historyKey(num shardcfg.Tnum) string {
	return fmt.Sprintf("config/%d", num)
}

func (sck *ShardCtrler) archive(cfg *shardcfg.ShardConfig) {
	key, encoded := historyKey(cfg.Num), cfg.String()
	for {
		_, _, err := sck.Get(key)
		if err == rpc.OK {
			return
		}
		if err == rpc.ErrNoKey {
			if sck.Put(key, encoded, 0) == rpc.OK {
				return
			}
			continue
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// publish advances the visible configuration without allowing a stale
// controller to overwrite a newer one.
func (sck *ShardCtrler) publish(cfg *shardcfg.ShardConfig) {
	encoded := cfg.String()
	for {
		value, version, err := sck.Get(configKey)
		if err == rpc.ErrNoKey {
			sck.archive(cfg)
			if sck.Put(configKey, encoded, 0) == rpc.OK {
				return
			}
			continue
		}
		if err != rpc.OK {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if shardcfg.FromString(value).Num >= cfg.Num {
			return
		}
		sck.archive(cfg)
		if sck.Put(configKey, encoded, version) == rpc.OK {
			return
		}
	}
}

func (sck *ShardCtrler) superseded(num shardcfg.Tnum) bool {
	current := sck.Query()
	return current != nil && current.Num >= num
}

func (sck *ShardCtrler) apply(next *shardcfg.ShardConfig) {
	current := sck.Query()
	if current == nil {
		sck.publish(next)
		return
	}
	if current.Num >= next.Num {
		return
	}

	for sh := shardcfg.Tshid(0); sh < shardcfg.NShards; sh++ {
		oldGID, oldServers, oldOK := current.GidServers(sh)
		newGID, newServers, newOK := next.GidServers(sh)
		if !oldOK || !newOK || oldGID == newGID {
			continue
		}

		oldClerk := shardgrp.MakeClerk(sck.clnt, oldServers)
		newClerk := shardgrp.MakeClerk(sck.clnt, newServers)
		for !sck.superseded(next.Num) {
			state, err := oldClerk.FreezeShard(sh, next.Num)
			if err == rpc.OK && newClerk.InstallShard(sh, state, next.Num) == rpc.OK {
				_ = oldClerk.DeleteShard(sh, next.Num)
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if sck.superseded(next.Num) {
			return
		}
	}
	sck.publish(next)
}

func (sck *ShardCtrler) ChangeConfigTo(next *shardcfg.ShardConfig) {
	encoded := next.String()
	for {
		value, version, err := sck.Get(nextKey)
		if err == rpc.ErrNoKey {
			if sck.Put(nextKey, encoded, 0) == rpc.OK {
				sck.apply(next)
				return
			}
			continue
		}
		if err != rpc.OK {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		chosen := shardcfg.FromString(value)
		if chosen.Num > next.Num || chosen.Num == next.Num && value != encoded {
			return
		}
		if chosen.Num == next.Num {
			sck.apply(chosen)
			return
		}
		if sck.Put(nextKey, encoded, version) == rpc.OK {
			sck.apply(next)
			return
		}
	}
}

func (sck *ShardCtrler) Query() *shardcfg.ShardConfig {
	return sck.get(configKey)
}

// QueryNum returns a completed historical configuration.
func (sck *ShardCtrler) QueryNum(num shardcfg.Tnum) *shardcfg.ShardConfig {
	current := sck.Query()
	if current == nil || num > current.Num {
		return nil
	}
	return sck.get(historyKey(num))
}
