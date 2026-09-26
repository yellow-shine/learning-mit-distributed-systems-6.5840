package kvraft

import (
	"bytes"

	"6.5840/kvraft1/rsm"
	"6.5840/kvsrv1/rpc"
	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/tester1"
)

type entry struct {
	Value   string
	Version rpc.Tversion
}

type KVServer struct {
	me   int
	rsm  *rsm.RSM
	data map[string]entry
}

func (kv *KVServer) DoOp(req any) any {
	switch a := req.(type) {
	case rpc.GetArgs:
		e, ok := kv.data[a.Key]
		if !ok {
			return rpc.GetReply{Err: rpc.ErrNoKey}
		}
		return rpc.GetReply{Value: e.Value, Version: e.Version, Err: rpc.OK}
	case rpc.PutArgs:
		e, ok := kv.data[a.Key]
		if !ok {
			if a.Version == 0 {
				kv.data[a.Key] = entry{a.Value, 1}
				return rpc.PutReply{Err: rpc.OK}
			}
			return rpc.PutReply{Err: rpc.ErrNoKey}
		}
		if e.Version != a.Version {
			return rpc.PutReply{Err: rpc.ErrVersion}
		}
		kv.data[a.Key] = entry{a.Value, e.Version + 1}
		return rpc.PutReply{Err: rpc.OK}
	default:
		return nil
	}
}

func (kv *KVServer) Snapshot() []byte {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(kv.data)
	return w.Bytes()
}

func (kv *KVServer) Restore(data []byte) {
	d := labgob.NewDecoder(bytes.NewBuffer(data))
	var m map[string]entry
	if d.Decode(&m) != nil {
		return
	}
	kv.data = m
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

func StartKVServer(servers []*labrpc.ClientEnd, gid tester.Tgid, me int, persister *tester.Persister, maxraftstate int) []any {
	labgob.Register(rsm.Op{})
	labgob.Register(rpc.PutArgs{})
	labgob.Register(rpc.GetArgs{})
	labgob.Register(rpc.GetReply{})
	labgob.Register(rpc.PutReply{})

	kv := &KVServer{me: me, data: make(map[string]entry)}

	kv.rsm = rsm.MakeRSM(servers, me, persister, maxraftstate, kv)
	return []any{kv, kv.rsm.Raft()}
}

func NewServer(tc *tester.TesterClnt, ends []*labrpc.ClientEnd, grp tester.Tgid, srv int, persister *tester.Persister) []any {
	return StartKVServer(ends, Gid, srv, persister, tester.MaxRaftState)
}
