package shardgrp

import (
	"bytes"
	"sync"

	"6.5840/kvraft1/rsm"
	"6.5840/kvsrv1/rpc"
	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/shardkv1/shardcfg"
	"6.5840/shardkv1/shardgrp/shardrpc"
	"6.5840/tester1"
)

const (
	ENVKEY = "65840ENV"
)

type entry struct {
	Value   string
	Version rpc.Tversion
}

type shardStatus int

const (
	shardAbsent shardStatus = iota
	shardServing
	shardFrozen
)

type shard struct {
	Status shardStatus
	Num    shardcfg.Tnum
	Data   map[string]entry
}

type KVServer struct {
	me   int
	rsm  *rsm.RSM
	gid  tester.Tgid
	mu   sync.Mutex
	shds map[shardcfg.Tshid]*shard
}

func (kv *KVServer) shard(s shardcfg.Tshid) *shard {
	sh, ok := kv.shds[s]
	if !ok {
		sh = &shard{Status: shardAbsent, Data: make(map[string]entry)}
		kv.shds[s] = sh
	}
	return sh
}

func (kv *KVServer) DoOp(req any) any {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	switch a := req.(type) {
	case rpc.GetArgs:
		sh := kv.shard(shardcfg.Key2Shard(a.Key))
		if sh.Status != shardServing {
			return rpc.GetReply{Err: rpc.ErrWrongGroup}
		}
		e, ok := sh.Data[a.Key]
		if !ok {
			return rpc.GetReply{Err: rpc.ErrNoKey}
		}
		return rpc.GetReply{Value: e.Value, Version: e.Version, Err: rpc.OK}
	case rpc.PutArgs:
		sh := kv.shard(shardcfg.Key2Shard(a.Key))
		if sh.Status != shardServing {
			return rpc.PutReply{Err: rpc.ErrWrongGroup}
		}
		e, ok := sh.Data[a.Key]
		if !ok {
			if a.Version == 0 {
				sh.Data[a.Key] = entry{a.Value, 1}
				return rpc.PutReply{Err: rpc.OK}
			}
			return rpc.PutReply{Err: rpc.ErrNoKey}
		}
		if e.Version != a.Version {
			return rpc.PutReply{Err: rpc.ErrVersion}
		}
		sh.Data[a.Key] = entry{a.Value, e.Version + 1}
		return rpc.PutReply{Err: rpc.OK}
	case shardrpc.FreezeShardArgs:
		sh := kv.shard(a.Shard)
		if a.Num < sh.Num {
			return shardrpc.FreezeShardReply{Err: rpc.ErrWrongGroup, Num: sh.Num}
		}
		sh.Status = shardFrozen
		sh.Num = a.Num
		w := new(bytes.Buffer)
		labgob.NewEncoder(w).Encode(sh.Data)
		return shardrpc.FreezeShardReply{State: w.Bytes(), Num: sh.Num, Err: rpc.OK}
	case shardrpc.InstallShardArgs:
		sh := kv.shard(a.Shard)
		if a.Num < sh.Num {
			return shardrpc.InstallShardReply{Err: rpc.ErrWrongGroup}
		}
		if a.Num == sh.Num && sh.Status == shardServing {
			return shardrpc.InstallShardReply{Err: rpc.OK}
		}
		data := make(map[string]entry)
		if len(a.State) > 0 {
			labgob.NewDecoder(bytes.NewBuffer(a.State)).Decode(&data)
		}
		sh.Data = data
		sh.Status = shardServing
		sh.Num = a.Num
		return shardrpc.InstallShardReply{Err: rpc.OK}
	case shardrpc.DeleteShardArgs:
		sh := kv.shard(a.Shard)
		if a.Num < sh.Num {
			return shardrpc.DeleteShardReply{Err: rpc.ErrWrongGroup}
		}
		sh.Status = shardAbsent
		sh.Data = make(map[string]entry)
		sh.Num = a.Num
		return shardrpc.DeleteShardReply{Err: rpc.OK}
	default:
		return nil
	}
}

func (kv *KVServer) Snapshot() []byte {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	w := new(bytes.Buffer)
	labgob.NewEncoder(w).Encode(kv.shds)
	return w.Bytes()
}

func (kv *KVServer) Restore(data []byte) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	var m map[shardcfg.Tshid]*shard
	if labgob.NewDecoder(bytes.NewBuffer(data)).Decode(&m) != nil {
		return
	}
	kv.shds = m
}

func (kv *KVServer) Get(args *rpc.GetArgs, reply *rpc.GetReply) {
	err, v := kv.rsm.Submit(*args)
	if err != rpc.OK {
		reply.Err = err
		return
	}
	*reply = v.(rpc.GetReply)
}

func (kv *KVServer) Put(args *rpc.PutArgs, reply *rpc.PutReply) {
	err, v := kv.rsm.Submit(*args)
	if err != rpc.OK {
		reply.Err = err
		return
	}
	*reply = v.(rpc.PutReply)
}

func (kv *KVServer) FreezeShard(args *shardrpc.FreezeShardArgs, reply *shardrpc.FreezeShardReply) {
	err, v := kv.rsm.Submit(*args)
	if err != rpc.OK {
		reply.Err = err
		return
	}
	*reply = v.(shardrpc.FreezeShardReply)
}

func (kv *KVServer) InstallShard(args *shardrpc.InstallShardArgs, reply *shardrpc.InstallShardReply) {
	err, v := kv.rsm.Submit(*args)
	if err != rpc.OK {
		reply.Err = err
		return
	}
	*reply = v.(shardrpc.InstallShardReply)
}

func (kv *KVServer) DeleteShard(args *shardrpc.DeleteShardArgs, reply *shardrpc.DeleteShardReply) {
	err, v := kv.rsm.Submit(*args)
	if err != rpc.OK {
		reply.Err = err
		return
	}
	*reply = v.(shardrpc.DeleteShardReply)
}

func StartServerShardGrp(servers []*labrpc.ClientEnd, gid tester.Tgid, me int, persister *tester.Persister, maxraftstate int) []any {
	labgob.Register(rpc.PutArgs{})
	labgob.Register(rpc.GetArgs{})
	labgob.Register(shardrpc.FreezeShardArgs{})
	labgob.Register(shardrpc.InstallShardArgs{})
	labgob.Register(shardrpc.DeleteShardArgs{})
	labgob.Register(rsm.Op{})
	labgob.Register(map[shardcfg.Tshid]*shard{})
	labgob.Register(shard{})
	labgob.Register(entry{})

	kv := &KVServer{gid: gid, me: me, shds: make(map[shardcfg.Tshid]*shard)}
	if gid == shardcfg.Gid1 {
		for s := shardcfg.Tshid(0); s < shardcfg.NShards; s++ {
			kv.shds[s] = &shard{Status: shardServing, Data: make(map[string]entry)}
		}
	}
	kv.rsm = rsm.MakeRSM(servers, me, persister, maxraftstate, kv)
	return []any{kv, kv.rsm.Raft()}
}

func NewServer(tc *tester.TesterClnt, ends []*labrpc.ClientEnd, grp tester.Tgid, srv int, persister *tester.Persister) []any {
	return StartServerShardGrp(ends, grp, srv, persister, tester.MaxRaftState)
}
