package shardkv

import (
	"sync"
	"time"

	"6.5840/kvsrv1/rpc"
	"6.5840/kvtest1"
	"6.5840/shardkv1/shardcfg"
	"6.5840/shardkv1/shardctrler"
	"6.5840/shardkv1/shardgrp"
	"6.5840/tester1"
)

type Clerk struct {
	clnt *tester.Clnt
	sck  *shardctrler.ShardCtrler
	mu   sync.Mutex
	rcks map[tester.Tgid]*shardgrp.Clerk
	cfg  *shardcfg.ShardConfig
}

func MakeClerk(clnt *tester.Clnt, sck *shardctrler.ShardCtrler) kvtest.IKVClerk {
	ck := &Clerk{clnt: clnt, sck: sck, rcks: make(map[tester.Tgid]*shardgrp.Clerk)}
	return ck
}

func (ck *Clerk) GetClerk(gid tester.Tgid) (*shardgrp.Clerk, bool) {
	ck.mu.Lock()
	defer ck.mu.Unlock()
	rck, ok := ck.rcks[gid]
	return rck, ok
}

func (ck *Clerk) groupClerk(gid tester.Tgid, srvs []string) *shardgrp.Clerk {
	ck.mu.Lock()
	defer ck.mu.Unlock()
	if c, ok := ck.rcks[gid]; ok {
		return c
	}
	c := shardgrp.MakeClerk(ck.clnt, srvs)
	ck.rcks[gid] = c
	return c
}

func (ck *Clerk) config() *shardcfg.ShardConfig {
	ck.mu.Lock()
	cfg := ck.cfg
	ck.mu.Unlock()
	if cfg != nil {
		return cfg
	}
	cfg = ck.sck.Query()
	if cfg != nil {
		ck.mu.Lock()
		ck.cfg = cfg
		ck.mu.Unlock()
	}
	return cfg
}

func (ck *Clerk) invalidate() {
	ck.mu.Lock()
	ck.cfg = nil
	ck.mu.Unlock()
}

func (ck *Clerk) Get(key string) (string, rpc.Tversion, rpc.Err) {
	for {
		cfg := ck.config()
		if cfg == nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		gid, srvs, ok := cfg.GidServers(shardcfg.Key2Shard(key))
		if !ok {
			ck.invalidate()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		val, ver, err := ck.groupClerk(gid, srvs).Get(key)
		if err == rpc.ErrWrongGroup || err == rpc.ErrWrongLeader {
			ck.invalidate()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return val, ver, err
	}
}

func (ck *Clerk) Put(key string, value string, version rpc.Tversion) rpc.Err {
	retried := false
	for {
		cfg := ck.config()
		if cfg == nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		gid, srvs, ok := cfg.GidServers(shardcfg.Key2Shard(key))
		if !ok {
			ck.invalidate()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		err := ck.groupClerk(gid, srvs).Put(key, value, version)
		if err == rpc.ErrWrongGroup || err == rpc.ErrWrongLeader {
			retried = true
			ck.invalidate()
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if err == rpc.ErrVersion && retried {
			return rpc.ErrMaybe
		}
		if err == rpc.ErrMaybe {
			retried = true
			continue
		}
		return err
	}
}
