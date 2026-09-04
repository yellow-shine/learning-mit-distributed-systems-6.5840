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

	// Your data here (3A, 3B, 3C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	applyCh         chan raftapi.ApplyMsg
	applyCond       *sync.Cond
	currentTerm     int
	votedFor        int
	log             []LogEntry
	commitIndex     int
	lastApplied     int
	nextIndex       []int
	matchIndex      []int
	role            int
	electionTimeout time.Duration
	lastReset       time.Time
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

func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	rf.persister.Save(w.Bytes(), nil)
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
	if d.Decode(&term) != nil || d.Decode(&votedFor) != nil || d.Decode(&log) != nil {
		return
	}
	rf.currentTerm = term
	rf.votedFor = votedFor
	rf.log = log
}

func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).
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
	i := len(rf.log) - 1
	return i, rf.log[i].Term
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
	if args.PrevLogIndex >= len(rf.log) {
		reply.Success = false
		reply.ConflictIndex = len(rf.log)
		reply.ConflictTerm = -1
		return
	}
	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Success = false
		reply.ConflictTerm = rf.log[args.PrevLogIndex].Term
		idx := args.PrevLogIndex
		for idx > 0 && rf.log[idx-1].Term == reply.ConflictTerm {
			idx--
		}
		reply.ConflictIndex = idx
		return
	}

	for i, e := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		if idx < len(rf.log) {
			if rf.log[idx].Term != e.Term {
				rf.log = rf.log[:idx]
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

func (rf *Raft) maybeCommitLocked() {
	for n := len(rf.log) - 1; n > rf.commitIndex; n-- {
		if rf.log[n].Term != rf.currentTerm {
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
		next := rf.nextIndex[i]
		if next < 1 {
			next = 1
		}
		if next > len(rf.log) {
			next = len(rf.log)
		}
		prev := next - 1
		args := AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: prev,
			PrevLogTerm:  rf.log[prev].Term,
			Entries:      append([]LogEntry{}, rf.log[next:]...),
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
			for j := len(rf.log) - 1; j >= 0; j-- {
				if rf.log[j].Term == reply.ConflictTerm {
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
					last := len(rf.log)
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
	index := len(rf.log) - 1
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
		entries := append([]LogEntry{}, rf.log[start:end+1]...)
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

	go rf.ticker()
	go rf.applier()

	return rf
}
