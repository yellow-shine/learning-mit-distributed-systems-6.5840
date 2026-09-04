package shardgrp

import (
	"6.5840/kvsrv1/rpc"
	"6.5840/shardkv1/shardcfg"
	"6.5840/shardkv1/shardgrp/shardrpc"
	"6.5840/tester1"
)

type Clerk struct {
	*tester.Clnt
	servers []string
	leader  int
}

func MakeClerk(clnt *tester.Clnt, servers []string) *Clerk {
	ck := &Clerk{Clnt: clnt, servers: servers}
	return ck
}

func (ck *Clerk) Leader() int {
	return ck.leader
}

func (ck *Clerk) Get(key string) (string, rpc.Tversion, rpc.Err) {
	args := rpc.GetArgs{Key: key}
	for i := 0; i < len(ck.servers); i++ {
		srv := (ck.leader + i) % len(ck.servers)
		reply := rpc.GetReply{}
		if ck.Call(ck.servers[srv], "KVServer.Get", &args, &reply) {
			if reply.Err != rpc.ErrWrongLeader {
				ck.leader = srv
				return reply.Value, reply.Version, reply.Err
			}
		}
	}
	return "", 0, rpc.ErrWrongLeader
}

func (ck *Clerk) Put(key string, value string, version rpc.Tversion) rpc.Err {
	args := rpc.PutArgs{Key: key, Value: value, Version: version}
	retried := false
	for i := 0; i < len(ck.servers); i++ {
		srv := (ck.leader + i) % len(ck.servers)
		reply := rpc.PutReply{}
		if ck.Call(ck.servers[srv], "KVServer.Put", &args, &reply) {
			if reply.Err == rpc.ErrWrongLeader {
				continue
			}
			ck.leader = srv
			if reply.Err == rpc.ErrVersion && retried {
				return rpc.ErrMaybe
			}
			return reply.Err
		}
		retried = true
	}
	return rpc.ErrWrongLeader
}

func (ck *Clerk) FreezeShard(s shardcfg.Tshid, num shardcfg.Tnum) ([]byte, rpc.Err) {
	args := shardrpc.FreezeShardArgs{Shard: s, Num: num}
	for i := 0; i < len(ck.servers); i++ {
		srv := (ck.leader + i) % len(ck.servers)
		reply := shardrpc.FreezeShardReply{}
		if ck.Call(ck.servers[srv], "KVServer.FreezeShard", &args, &reply) && reply.Err != rpc.ErrWrongLeader {
			ck.leader = srv
			return reply.State, reply.Err
		}
	}
	return nil, rpc.ErrWrongLeader
}

func (ck *Clerk) InstallShard(s shardcfg.Tshid, state []byte, num shardcfg.Tnum) rpc.Err {
	args := shardrpc.InstallShardArgs{Shard: s, State: state, Num: num}
	for i := 0; i < len(ck.servers); i++ {
		srv := (ck.leader + i) % len(ck.servers)
		reply := shardrpc.InstallShardReply{}
		if ck.Call(ck.servers[srv], "KVServer.InstallShard", &args, &reply) && reply.Err != rpc.ErrWrongLeader {
			ck.leader = srv
			return reply.Err
		}
	}
	return rpc.ErrWrongLeader
}

func (ck *Clerk) DeleteShard(s shardcfg.Tshid, num shardcfg.Tnum) rpc.Err {
	args := shardrpc.DeleteShardArgs{Shard: s, Num: num}
	for i := 0; i < len(ck.servers); i++ {
		srv := (ck.leader + i) % len(ck.servers)
		reply := shardrpc.DeleteShardReply{}
		if ck.Call(ck.servers[srv], "KVServer.DeleteShard", &args, &reply) && reply.Err != rpc.ErrWrongLeader {
			ck.leader = srv
			return reply.Err
		}
	}
	return rpc.ErrWrongLeader
}
