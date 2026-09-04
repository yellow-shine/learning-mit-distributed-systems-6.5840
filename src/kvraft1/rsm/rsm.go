package rsm

import (
	"sync"
	"sync/atomic"

	"6.5840/kvsrv1/rpc"
	"6.5840/labrpc"
	"6.5840/raft1"
	"6.5840/raftapi"
	"6.5840/tester1"
)

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Me  int
	Id  int64
	Req any
}

// A server (i.e., ../server.go) that wants to replicate itself calls
// MakeRSM and must implement the StateMachine interface.  This
// interface allows the rsm package to interact with the server for
// server-specific operations: the server must implement DoOp to
// execute an operation (e.g., a Get or Put request), and
// Snapshot/Restore to snapshot and restore the server's state.
type StateMachine interface {
	DoOp(any) any
	Snapshot() []byte
	Restore([]byte)
}

type submitWait struct {
	id int64
	ch chan submitResult
}

type submitResult struct {
	err rpc.Err
	v   any
}

type RSM struct {
	mu           sync.Mutex
	me           int
	rf           raftapi.Raft
	applyCh      chan raftapi.ApplyMsg
	maxraftstate int // snapshot if log grows this big
	sm           StateMachine
	// Your definitions here.
	id      int64
	pending map[int]submitWait
}

func MakeRSM(servers []*labrpc.ClientEnd, me int, persister *tester.Persister, maxraftstate int, sm StateMachine) *RSM {
	rsm := &RSM{
		me:           me,
		maxraftstate: maxraftstate,
		applyCh:      make(chan raftapi.ApplyMsg),
		sm:           sm,
		pending:      make(map[int]submitWait),
	}
	if snap := persister.ReadSnapshot(); len(snap) > 0 {
		sm.Restore(snap)
	}
	if !tester.UseRaftStateMachine {
		rsm.rf = raft.Make(servers, me, persister, rsm.applyCh)
	}
	go rsm.reader()
	return rsm
}

func (rsm *RSM) Raft() raftapi.Raft {
	return rsm.rf
}

func (rsm *RSM) reader() {
	for msg := range rsm.applyCh {
		if msg.SnapshotValid {
			rsm.sm.Restore(msg.Snapshot)
			continue
		}
		if !msg.CommandValid {
			continue
		}
		op := msg.Command.(Op)
		ret := rsm.sm.DoOp(op.Req)
		_, isLeader := rsm.rf.GetState()
		rsm.mu.Lock()
		if w, ok := rsm.pending[msg.CommandIndex]; ok {
			if w.id == op.Id && op.Me == rsm.me {
				w.ch <- submitResult{rpc.OK, ret}
			} else {
				w.ch <- submitResult{rpc.ErrWrongLeader, nil}
			}
			delete(rsm.pending, msg.CommandIndex)
		}
		if !isLeader {
			for idx, w := range rsm.pending {
				w.ch <- submitResult{rpc.ErrWrongLeader, nil}
				delete(rsm.pending, idx)
			}
		}
		rsm.mu.Unlock()
		if rsm.maxraftstate > 0 && rsm.rf.PersistBytes() >= rsm.maxraftstate {
			rsm.rf.Snapshot(msg.CommandIndex, rsm.sm.Snapshot())
		}
	}
	rsm.mu.Lock()
	for idx, w := range rsm.pending {
		w.ch <- submitResult{rpc.ErrWrongLeader, nil}
		delete(rsm.pending, idx)
	}
	rsm.mu.Unlock()
}

// Submit a command to Raft, and wait for it to be committed.  It
// should return ErrWrongLeader if client should find new leader and
// try again.
func (rsm *RSM) Submit(req any) (rpc.Err, any) {

	// Submit creates an Op structure to run a command through Raft;
	// for example: op := Op{Me: rsm.me, Id: id, Req: req}, where req
	// is the argument to Submit and id is a unique id for the op.

	id := atomic.AddInt64(&rsm.id, 1)
	op := Op{Me: rsm.me, Id: id, Req: req}
	index, _, isLeader := rsm.rf.Start(op)
	if !isLeader {
		return rpc.ErrWrongLeader, nil
	}
	ch := make(chan submitResult, 1)
	rsm.mu.Lock()
	rsm.pending[index] = submitWait{id: id, ch: ch}
	rsm.mu.Unlock()
	res := <-ch
	return res.err, res.v
}
