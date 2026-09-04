package kvraft

import (
	"time"

	"6.5840/kvsrv1/rpc"
	"6.5840/kvtest1"
	"6.5840/tester1"
)

type Clerk struct {
	clnt    *tester.Clnt
	servers []string
	leader  int
}

func MakeClerk(clnt *tester.Clnt, servers []string) kvtest.IKVClerk {
	ck := &Clerk{clnt: clnt, servers: servers}
	return ck
}

func (ck *Clerk) Leader() int {
	return ck.leader
}

func (ck *Clerk) Get(key string) (string, rpc.Tversion, rpc.Err) {
	args := rpc.GetArgs{Key: key}
	for {
		for i := 0; i < len(ck.servers); i++ {
			srv := (ck.leader + i) % len(ck.servers)
			type res struct {
				ok    bool
				reply rpc.GetReply
			}
			ch := make(chan res, 1)
			go func(srv int) {
				reply := rpc.GetReply{}
				ok := ck.clnt.Call(ck.servers[srv], "KVServer.Get", &args, &reply)
				ch <- res{ok, reply}
			}(srv)
			select {
			case r := <-ch:
				if r.ok && r.reply.Err != rpc.ErrWrongLeader {
					ck.leader = srv
					return r.reply.Value, r.reply.Version, r.reply.Err
				}
			case <-time.After(100 * time.Millisecond):
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (ck *Clerk) Put(key string, value string, version rpc.Tversion) rpc.Err {
	args := rpc.PutArgs{Key: key, Value: value, Version: version}
	retried := false
	for {
		for i := 0; i < len(ck.servers); i++ {
			srv := (ck.leader + i) % len(ck.servers)
			type res struct {
				ok    bool
				reply rpc.PutReply
			}
			ch := make(chan res, 1)
			go func(srv int) {
				reply := rpc.PutReply{}
				ok := ck.clnt.Call(ck.servers[srv], "KVServer.Put", &args, &reply)
				ch <- res{ok, reply}
			}(srv)
			select {
			case r := <-ch:
				if !r.ok {
					retried = true
					continue
				}
				if r.reply.Err == rpc.ErrWrongLeader {
					continue
				}
				ck.leader = srv
				if r.reply.Err == rpc.ErrVersion && retried {
					return rpc.ErrMaybe
				}
				return r.reply.Err
			case <-time.After(100 * time.Millisecond):
				retried = true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
