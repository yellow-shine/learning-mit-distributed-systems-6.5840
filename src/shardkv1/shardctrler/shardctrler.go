package shardctrler

import (
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
	srv := tester.ServerName(tester.GRP0, 0)
	sck.IKVClerk = kvsrv.MakeClerk(clnt, srv)
	return sck
}

func (sck *ShardCtrler) InitController() {
	next := sck.get(nextKey)
	if next == nil {
		return
	}
	curr := sck.Query()
	if curr == nil || next.Num > curr.Num {
		sck.apply(next)
	}
}

func (sck *ShardCtrler) InitConfig(cfg *shardcfg.ShardConfig) {
	sck.Put(configKey, cfg.String(), 0)
}

func (sck *ShardCtrler) get(key string) *shardcfg.ShardConfig {
	v, _, err := sck.Get(key)
	if err != rpc.OK || v == "" {
		return nil
	}
	return shardcfg.FromString(v)
}

func (sck *ShardCtrler) put(key string, cfg *shardcfg.ShardConfig) {
	for {
		_, ver, err := sck.Get(key)
		if err == rpc.ErrNoKey {
			if sck.Put(key, cfg.String(), 0) == rpc.OK {
				return
			}
			continue
		}
		if err != rpc.OK {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if sck.Put(key, cfg.String(), ver) == rpc.OK {
			return
		}
	}
}

func (sck *ShardCtrler) apply(new *shardcfg.ShardConfig) {
	old := sck.Query()
	if old == nil {
		sck.put(configKey, new)
		return
	}
	if old.Num >= new.Num {
		return
	}
	for sh := shardcfg.Tshid(0); sh < shardcfg.NShards; sh++ {
		og, osrv, ook := old.GidServers(sh)
		ng, nsrv, nok := new.GidServers(sh)
		if !ook || !nok || og == ng {
			continue
		}
		ock := shardgrp.MakeClerk(sck.clnt, osrv)
		nck := shardgrp.MakeClerk(sck.clnt, nsrv)
		for {
			state, err := ock.FreezeShard(sh, new.Num)
			if err == rpc.OK {
				if nck.InstallShard(sh, state, new.Num) == rpc.OK {
					ock.DeleteShard(sh, new.Num)
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	sck.put(configKey, new)
}

func (sck *ShardCtrler) ChangeConfigTo(new *shardcfg.ShardConfig) {
	sck.put(nextKey, new)
	sck.apply(new)
}

func (sck *ShardCtrler) Query() *shardcfg.ShardConfig {
	return sck.get(configKey)
}
