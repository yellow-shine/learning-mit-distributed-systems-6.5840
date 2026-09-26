package raft

// The file ../raftapi/raftapi.go defines the interface that raft must
// expose to servers (or the tester), but see comments below for each
// of these functions for more details.
//
// In addition,  Make() creates a new raft peer that implements the
// raft interface.

import (
	"bytes"
	"math/rand"
	"sync"
	"time"

	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raftapi"
	"6.5840/tester1"
)

const (
	follower = iota
	candidate
	leader
)

const heartbeatInterval = 100 * time.Millisecond

type LogEntry struct {
	Term    int
	Command interface{}
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *tester.Persister   // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]

	applyCh           chan raftapi.ApplyMsg
	applyCond         *sync.Cond
	currentTerm       int
	votedFor          int
	log               []LogEntry // log[0] is dummy at lastIncludedIndex
	lastIncludedIndex int
	lastIncludedTerm  int
	snapshot          []byte
	commitIndex       int
	lastApplied       int
	nextIndex         []int
	matchIndex        []int
	role              int
	electionTimeout   time.Duration
	lastReset         time.Time
}

func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.role == leader
}

func (rf *Raft) resetElectionLocked() {
	rf.lastReset = time.Now()
	rf.electionTimeout = time.Duration(300+rand.Intn(200)) * time.Millisecond
}

func (rf *Raft) becomeFollowerLocked(term int) {
	if term > rf.currentTerm {
		rf.currentTerm = term
		rf.votedFor = -1
		rf.persist()
	}
	rf.role = follower
	rf.resetElectionLocked()
}

func (rf *Raft) lastIndexLocked() int {
	return rf.lastIncludedIndex + len(rf.log) - 1
}

func (rf *Raft) termLocked(i int) int {
	return rf.log[i-rf.lastIncludedIndex].Term
}

func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	rf.persister.Save(w.Bytes(), rf.snapshot)
}

func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 {
		return
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var term int
	var votedFor int
	var log []LogEntry
	var lastIdx int
	var lastTerm int
	if d.Decode(&term) != nil || d.Decode(&votedFor) != nil || d.Decode(&log) != nil ||
		d.Decode(&lastIdx) != nil || d.Decode(&lastTerm) != nil {
		return
	}
	rf.currentTerm = term
	rf.votedFor = votedFor
	rf.log = log
	rf.lastIncludedIndex = lastIdx
	rf.lastIncludedTerm = lastTerm
}

func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

func (rf *Raft) Snapshot(index int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if index <= rf.lastIncludedIndex || index > rf.commitIndex {
		return
	}
	off := index - rf.lastIncludedIndex
	rf.log = append([]LogEntry{}, rf.log[off:]...)
	rf.lastIncludedIndex = index
	rf.lastIncludedTerm = rf.log[0].Term
	rf.snapshot = snapshot
	if rf.lastApplied < index {
		rf.lastApplied = index
	}
	rf.persist()
}

type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

func (rf *Raft) lastLogLocked() (int, int) {
	i := rf.lastIndexLocked()
	return i, rf.termLocked(i)
}

func moreUpToDate(candLastIdx, candLastTerm, myLastIdx, myLastTerm int) bool {
	if candLastTerm != myLastTerm {
		return candLastTerm > myLastTerm
	}
	return candLastIdx >= myLastIdx
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term > rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	}
	reply.Term = rf.currentTerm
	reply.VoteGranted = false
	if args.Term < rf.currentTerm {
		return
	}
	lastIdx, lastTerm := rf.lastLogLocked()
	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) &&
		moreUpToDate(args.LastLogIndex, args.LastLogTerm, lastIdx, lastTerm) {
		rf.votedFor = args.CandidateId
		rf.persist()
		rf.resetElectionLocked()
		reply.VoteGranted = true
	}
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term          int
	Success       bool
	ConflictIndex int
	ConflictTerm  int
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = false
		return
	}
	if args.Term > rf.currentTerm || rf.role != follower {
		rf.becomeFollowerLocked(args.Term)
	} else {
		rf.resetElectionLocked()
	}

	reply.Term = rf.currentTerm
	if args.PrevLogIndex < rf.lastIncludedIndex {
		reply.Success = false
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		reply.ConflictTerm = -1
		return
	}
	if args.PrevLogIndex > rf.lastIndexLocked() {
		reply.Success = false
		reply.ConflictIndex = rf.lastIndexLocked() + 1
		reply.ConflictTerm = -1
		return
	}
	if rf.termLocked(args.PrevLogIndex) != args.PrevLogTerm {
		reply.Success = false
		reply.ConflictTerm = rf.termLocked(args.PrevLogIndex)
		idx := args.PrevLogIndex
		for idx > rf.lastIncludedIndex && rf.termLocked(idx-1) == reply.ConflictTerm {
			idx--
		}
		reply.ConflictIndex = idx
		return
	}

	for i, e := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		off := idx - rf.lastIncludedIndex
		if off < len(rf.log) {
			if rf.log[off].Term != e.Term {
				rf.log = rf.log[:off]
				rf.log = append(rf.log, args.Entries[i:]...)
				break
			}
		} else {
			rf.log = append(rf.log, args.Entries[i:]...)
			break
		}
	}
	if len(args.Entries) > 0 {
		rf.persist()
	}

	if args.LeaderCommit > rf.commitIndex {
		lastNew := args.PrevLogIndex + len(args.Entries)
		if lastNew > rf.commitIndex {
			if args.LeaderCommit < lastNew {
				rf.commitIndex = args.LeaderCommit
			} else {
				rf.commitIndex = lastNew
			}
			rf.applyCond.Broadcast()
		}
	}
	reply.Success = true
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	return rf.peers[server].Call("Raft.AppendEntries", args, reply)
}

type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

type InstallSnapshotReply struct {
	Term int
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		rf.mu.Unlock()
		return
	}
	if args.Term > rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	} else {
		rf.resetElectionLocked()
	}
	reply.Term = rf.currentTerm
	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		rf.mu.Unlock()
		return
	}

	if args.LastIncludedIndex < rf.lastIndexLocked() &&
		rf.termLocked(args.LastIncludedIndex) == args.LastIncludedTerm {
		off := args.LastIncludedIndex - rf.lastIncludedIndex
		rf.log = append([]LogEntry{}, rf.log[off:]...)
	} else {
		rf.log = []LogEntry{{Term: args.LastIncludedTerm}}
	}
	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludedTerm = args.LastIncludedTerm
	rf.snapshot = args.Data
	if rf.commitIndex < args.LastIncludedIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	if rf.lastApplied < args.LastIncludedIndex {
		rf.lastApplied = args.LastIncludedIndex
	}
	rf.persist()
	msg := raftapi.ApplyMsg{
		SnapshotValid: true,
		Snapshot:      args.Data,
		SnapshotTerm:  args.LastIncludedTerm,
		SnapshotIndex: args.LastIncludedIndex,
	}
	rf.mu.Unlock()
	rf.applyCh <- msg
}

func (rf *Raft) maybeCommitLocked() {
	for n := rf.lastIndexLocked(); n > rf.commitIndex; n-- {
		if rf.termLocked(n) != rf.currentTerm {
			continue
		}
		count := 1
		for i := range rf.peers {
			if i != rf.me && rf.matchIndex[i] >= n {
				count++
			}
		}
		if count > len(rf.peers)/2 {
			rf.commitIndex = n
			rf.applyCond.Broadcast()
			return
		}
	}
}

func (rf *Raft) sendToPeer(i int) {
	for {
		rf.mu.Lock()
		if rf.role != leader {
			rf.mu.Unlock()
			return
		}
		if rf.nextIndex[i] <= rf.lastIncludedIndex {
			args := InstallSnapshotArgs{
				Term:              rf.currentTerm,
				LeaderId:          rf.me,
				LastIncludedIndex: rf.lastIncludedIndex,
				LastIncludedTerm:  rf.lastIncludedTerm,
				Data:              rf.snapshot,
			}
			term := rf.currentTerm
			rf.mu.Unlock()
			reply := InstallSnapshotReply{}
			ok := rf.peers[i].Call("Raft.InstallSnapshot", &args, &reply)
			rf.mu.Lock()
			if !ok {
				rf.mu.Unlock()
				return
			}
			if reply.Term > rf.currentTerm {
				rf.becomeFollowerLocked(reply.Term)
				rf.mu.Unlock()
				return
			}
			if rf.role != leader || rf.currentTerm != term {
				rf.mu.Unlock()
				return
			}
			rf.nextIndex[i] = args.LastIncludedIndex + 1
			rf.matchIndex[i] = args.LastIncludedIndex
			rf.maybeCommitLocked()
			rf.mu.Unlock()
			return
		}

		next := rf.nextIndex[i]
		if next < rf.lastIncludedIndex+1 {
			next = rf.lastIncludedIndex + 1
		}
		if next > rf.lastIndexLocked()+1 {
			next = rf.lastIndexLocked() + 1
		}
		prev := next - 1
		args := AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: prev,
			PrevLogTerm:  rf.termLocked(prev),
			Entries:      append([]LogEntry{}, rf.log[next-rf.lastIncludedIndex:]...),
			LeaderCommit: rf.commitIndex,
		}
		rf.mu.Unlock()

		reply := AppendEntriesReply{}
		if !rf.sendAppendEntries(i, &args, &reply) {
			return
		}

		rf.mu.Lock()
		if reply.Term > rf.currentTerm {
			rf.becomeFollowerLocked(reply.Term)
			rf.mu.Unlock()
			return
		}
		if rf.role != leader || rf.currentTerm != args.Term {
			rf.mu.Unlock()
			return
		}
		if reply.Success {
			rf.nextIndex[i] = args.PrevLogIndex + 1 + len(args.Entries)
			rf.matchIndex[i] = rf.nextIndex[i] - 1
			rf.maybeCommitLocked()
			rf.mu.Unlock()
			return
		}
		if reply.ConflictTerm < 0 {
			rf.nextIndex[i] = reply.ConflictIndex
		} else {
			last := -1
			for j := rf.lastIndexLocked(); j >= rf.lastIncludedIndex; j-- {
				if rf.termLocked(j) == reply.ConflictTerm {
					last = j
					break
				}
			}
			if last >= 0 {
				rf.nextIndex[i] = last + 1
			} else {
				rf.nextIndex[i] = reply.ConflictIndex
			}
		}
		if rf.nextIndex[i] < 1 {
			rf.nextIndex[i] = 1
		}
		rf.mu.Unlock()
	}
}

func (rf *Raft) broadcastAppend() {
	rf.mu.Lock()
	if rf.role != leader {
		rf.mu.Unlock()
		return
	}
	rf.mu.Unlock()
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go rf.sendToPeer(i)
	}
}

func (rf *Raft) startElection() {
	rf.mu.Lock()
	rf.role = candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.persist()
	term := rf.currentTerm
	lastIdx, lastTerm := rf.lastLogLocked()
	rf.resetElectionLocked()
	rf.mu.Unlock()

	votes := 1
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go func(i int) {
			args := RequestVoteArgs{
				Term:         term,
				CandidateId:  rf.me,
				LastLogIndex: lastIdx,
				LastLogTerm:  lastTerm,
			}
			reply := RequestVoteReply{}
			if !rf.sendRequestVote(i, &args, &reply) {
				return
			}
			rf.mu.Lock()
			defer rf.mu.Unlock()
			if reply.Term > rf.currentTerm {
				rf.becomeFollowerLocked(reply.Term)
				return
			}
			if rf.role != candidate || rf.currentTerm != term {
				return
			}
			if reply.VoteGranted {
				votes++
				if votes > len(rf.peers)/2 {
					rf.role = leader
					rf.nextIndex = make([]int, len(rf.peers))
					rf.matchIndex = make([]int, len(rf.peers))
					last := rf.lastIndexLocked() + 1
					for j := range rf.peers {
						rf.nextIndex[j] = last
						rf.matchIndex[j] = 0
					}
					go rf.broadcastAppend()
				}
			}
		}(i)
	}
}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	if rf.role != leader {
		term := rf.currentTerm
		rf.mu.Unlock()
		return -1, term, false
	}
	rf.log = append(rf.log, LogEntry{Term: rf.currentTerm, Command: command})
	rf.persist()
	index := rf.lastIndexLocked()
	term := rf.currentTerm
	rf.mu.Unlock()
	go rf.broadcastAppend()
	return index, term, true
}

func (rf *Raft) applier() {
	for {
		rf.mu.Lock()
		for rf.lastApplied >= rf.commitIndex {
			rf.applyCond.Wait()
		}
		start := rf.lastApplied + 1
		end := rf.commitIndex
		base := rf.lastIncludedIndex
		if start <= base {
			start = base + 1
		}
		var entries []LogEntry
		if start <= end {
			entries = append([]LogEntry{}, rf.log[start-base:end-base+1]...)
		}
		rf.lastApplied = end
		rf.mu.Unlock()
		for i, e := range entries {
			rf.applyCh <- raftapi.ApplyMsg{
				CommandValid: true,
				Command:      e.Command,
				CommandIndex: start + i,
			}
		}
	}
}

func (rf *Raft) ticker() {
	for {
		time.Sleep(10 * time.Millisecond)
		rf.mu.Lock()
		role := rf.role
		due := time.Since(rf.lastReset) >= rf.electionTimeout
		rf.mu.Unlock()
		if role == leader {
			rf.broadcastAppend()
			time.Sleep(heartbeatInterval)
		} else if due {
			rf.startElection()
		}
	}
}

func Make(peers []*labrpc.ClientEnd, me int,
	persister *tester.Persister, applyCh chan raftapi.ApplyMsg) raftapi.Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	rf.applyCh = applyCh
	rf.applyCond = sync.NewCond(&rf.mu)
	rf.votedFor = -1
	rf.log = []LogEntry{{Term: 0}}
	rf.role = follower
	rf.resetElectionLocked()

	rf.readPersist(persister.ReadRaftState())
	rf.snapshot = persister.ReadSnapshot()
	if rf.lastIncludedIndex > 0 {
		rf.commitIndex = rf.lastIncludedIndex
		rf.lastApplied = rf.lastIncludedIndex
	}

	go rf.ticker()
	go rf.applier()

	return rf
}
