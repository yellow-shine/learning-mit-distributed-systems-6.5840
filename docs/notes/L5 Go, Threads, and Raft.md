## MIT 6.824 Lecture 5 — Go, Threads, and Raft

这节课和 GFS、Raft、ZooKeeper 那些课有一点不一样：

> **它不是在发明一个新的 distributed algorithm，而是在教你：怎样把 distributed algorithm 写成一个正确的 concurrent program。**

MIT 的 Raft Lab 指南明确强调：一个 Raft peer 同时要处理 `Start()`、`RequestVote`、`AppendEntries`、RPC replies、election timeout、heartbeat 等事件，而这些事件来自不同 goroutine；课程建议 Raft 实现中通常采用 **shared state + lock** 的结构。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt)

---

# Part 1：这节课到底想解决什么问题？

如果只记住一个问题：

> **一个 goroutine 开始执行时看到的世界，经过 RPC / Sleep / Channel / Lock release 之后，还是原来的世界吗？**

答案是：

> **绝对不能这样假设。**

这其实是 Raft 实现中最容易出 bug 的地方。

假设 A 正在竞选：

```
A
currentTerm = 7
state       = Candidate
votes       = 1
```

然后：

```
A ── RequestVote(term=7) ──> B
```

RPC 很慢。

等待期间：

```
C ── AppendEntries(term=8) ──> A
```

于是 A 变成：

```
currentTerm = 8
state       = Follower
```

但最开始发送出去的 RPC 终于返回：

```
B ── voteGranted=true ──> A
```

如果旧 goroutine 直接：

```
votes++
if votes >= majority {
    state = Leader
}
```

你可能得到：

```
term 8:
A = Leader
```

但这个 vote 明明属于：

```
term 7 election
```

问题不是：

```
Raft 理论错误
```

也不是：

```
RPC 错误
```

而是：

```
旧 goroutine
     ↓
携带旧世界的 assumptions
     ↓
穿越了一段 blocking operation
     ↓
回来修改新世界
```

所以本课的核心 Mental Model 是：

```
Concurrency Bug
      ↓
Protocol State 被错误修改
      ↓
Protocol Invariant 被破坏
      ↓
Distributed Safety 被破坏
```

单机程序里 race 可能只是错误结果。

Distributed Protocol 里：

```
race
→ wrong term
→ wrong leader
→ wrong replication decision
→ potentially violate consensus safety
```

---

# Part 2：在整个 6.824 中的位置

把前后课程连起来：

```
Lecture 1
Distributed Systems
failure / concurrency / replication
        │
        ▼
Lecture 2
RPC + Threads
远程调用 + 本机并发
        │
        ▼
Lecture 3
GFS
真实 replicated storage
        │
        ▼
Lecture 4
Primary/Backup
怎样复制状态
        │
        ▼
┌──────────────────────────────┐
│ Lecture 5                    │
│ Go, Threads, and Raft        │
│                              │
│ 怎么正确实现并发协议？       │
└──────────────────────────────┘
        │
        ▼
Lecture 6 / 7
Raft
Leader Election
Log Replication
Safety
        │
        ▼
State Machine Replication
        │
        ▼
Linearizable KV / ZooKeeper
```

MIT 当前课程也仍然把 Go/concurrency 相关内容紧邻 Raft Lab；Raft Lab 的目标就是实现 replicated state machine，后续再在其上构建 fault-tolerant KV。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

这里尤其要分清几个层次：

```
goroutine / mutex / channel
        ↓
解决单个进程内部 concurrency

RPC
        ↓
解决跨进程通信

Raft
        ↓
解决多个节点如何对 ordered log 达成一致

State Machine Replication
        ↓
使用这个 ordered log
让多个 state machine 得到相同结果

Linearizability
        ↓
描述外部 client 观察到的行为规格
```

所以：

> **mutex 不解决 distributed consensus；Raft 也不解决你代码里的 data race。**

Raft 的数学证明默认：

```
每个节点正确执行 Raft transition rules
```

但你的 Go implementation 如果 race 了，这个前提就不成立。

---

# Part 3：六个核心 Mental Models

### Concept 1：goroutine ≠ distributed node

goroutine 是一个：

```
single process 内部的 concurrent execution
```

而 Raft node 是：

```
distributed protocol participant
```

一个 Raft node 里面可能有很多 goroutine：

```
Raft Peer A
│
├── election timer goroutine
├── heartbeat goroutine
├── RequestVote handler goroutine
├── AppendEntries handler goroutine
├── RPC sender B
├── RPC sender C
└── apply goroutine
```

它们共同访问：

```
currentTerm
votedFor
state
log[]
commitIndex
lastApplied
nextIndex[]
matchIndex[]
```

所以：

```
一个 distributed node
≠
一个 sequential thread
```

这是很多第一次写 Raft 的人最容易忽略的东西。

---

### Concept 2：Data Race 和 Logical Race 不一样

#### Data Race

两个 goroutine 无同步访问同一变量，至少一个写：

```
go func() {
    x++
}()

go func() {
    x++
}()
```

Go race detector 很擅长发现这类问题。

MIT 也明确建议：

```
go test -race
```

并修复 race detector 报出的 race。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html)

但还有更危险的：

#### Logical Race

所有变量访问都有 mutex。

依然错。

例如：

```
mu.Lock()
term := currentTerm
mu.Unlock()

reply := sendRPC(...)

mu.Lock()
state = Leader
mu.Unlock()
```

从 race detector 看：

```
✅ 没有 data race
```

但如果 RPC 过程中：

```
currentTerm:
7 → 8
```

那么最后一句：

```
state = Leader
```

逻辑上依然错误。

所以：

```
Race Detector
    │
    ├── 能发现：
    │   unsafe memory concurrency
    │
    └── 不能证明：
        protocol concurrency correctness
```

官方 locking advice 也特别提醒：race detector 对基本共享内存冲突很有帮助，但无法替你发现后续这些 locking/protocol 规则问题。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)

---

## Concept 3：Critical Section 的真正含义

很多人把 mutex 理解成：

> 防止两个 goroutine 同时修改变量。

不够。

更准确是：

> **保护一个 invariant。**

比如：

```
currentTerm++
state = Candidate
```

逻辑上这是一个 state transition：

```
Follower(term=6)
      ↓
Candidate(term=7)
```

不希望别人看到：

```
currentTerm = 7
state       = Follower
```

这种中间状态。

因此：

```
mu.Lock()

currentTerm++
state = Candidate

mu.Unlock()
```

这里 lock 保护的不是两个 variables。

保护的是：

```
(currentTerm, state)

之间的 consistency invariant
```

MIT locking guidance 直接用这一类例子说明：如果一组 shared-state modifications 不应该被其他 goroutine 看见中间状态，就应该把整个 sequence 放在同一 critical section 中。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)

---

## Concept 4：Lock protects transitions, not just variables

再看：

```
if args.Term > rf.currentTerm {
    rf.currentTerm = args.Term
}
```

假设没有锁。

两个 goroutine：

```
G1: args.Term = 8
G2: args.Term = 9
```

可能发生：

```
G1:
read currentTerm = 7
8 > 7

              G2:
              read currentTerm = 7
              9 > 7
              currentTerm = 9

G1:
currentTerm = 8
```

最终：

```
9 → 8
```

违反：

```
Invariant:

currentTerm monotonically increases.
```

因此需要保护的不是：

```
currentTerm = ...
```

而是：

```
check
  +
decision
  +
update
```

整体：

```
mu.Lock()

if args.Term > currentTerm {
    currentTerm = args.Term
}

mu.Unlock()
```

---

## Concept 5：不要持锁等待

这是本课最重要的工程规则之一。

危险操作包括：

```
RPC
channel receive
channel send
Sleep
timer wait
disk/network I/O
condition wait
```

为什么？

假设 A 和 B：

```
A                              B

Lock(A)

RPC B ----------------------->

                               handler:
                               Lock(B)

                               RPC A -------->
                                              handler:
                                              Lock(A)
                                              BLOCKED

等待 B reply

                    DEADLOCK
```

形成：

```
A holds A.mu
    ↓ waits
B RPC handler

B holds B.mu
    ↓ waits
A RPC handler
```

MIT 的建议非常明确：一般不要在持锁时执行可能 wait 的操作，包括 channel、sleep、timer 或 RPC；除了阻塞其他 goroutine progress，还容易导致 deadlock。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)

因此常见结构：

```
mu.Lock()

// snapshot what we need
term := currentTerm
args := makeArgsLocked()

mu.Unlock()

reply := sendRPC(args)

mu.Lock()

// revalidate
...

mu.Unlock()
```

但是这立刻引出下一问题。

---

## Concept 6：Unlock → Wait → Lock 是一个“时间旅行边界”

这是整节课最重要的一句话：

> **每一次 release lock + wait + reacquire lock，都必须假设世界可能完全改变。**

例如：

```
mu.Lock()

term := currentTerm
state := Candidate

mu.Unlock()

reply := RequestVote(...)

mu.Lock()
```

这时候你不能想：

```
“我刚才还是 Candidate。”
```

应该想：

```
“我离开了 critical section。

在我回来之前：
任何事情都可能发生。”
```

可能：

```
term 7 → 8 → 9

Candidate
→ Follower
→ Candidate
→ Leader
→ Follower
```

网络甚至可能让旧 RPC 晚几秒回来。

因此：

```
Before wait:
snapshot assumptions

After wait:
validate assumptions
```

这就是后面你会不断看到的模式。

---

# Part 4：System Model

这节课需要区分两个 model。

### Local concurrency model

一个 Raft process：

```
one address space

many goroutines

shared:
Raft struct
```

内存本身不是 replicated。

goroutine 通过：

```
mutex
channel
condition variable
```

同步。

---

### Distributed model

Raft context 中：

```
Node A
Node B
Node C
```

网络允许：

```
delay
loss
reordering
duplicate effects through retry
partition
```

而最关键的是：

> latency 没有一个你能够依赖的严格上界。

所以：

```
RPC timeout
```

不能证明：

```
remote node dead
```

只能说明：

```
“我这么久没有收到 response。”
```

---

### Failure Model

Raft 的正常 fault model 是：

```
crash / network failure
```

不是：

```
Byzantine
```

即节点不会恶意：

```
伪造 term
篡改 log
故意违反 protocol
```

但注意：

> **你的 buggy Go code，效果有时跟 Byzantine node 很像。**

比如：

```
错误发送 term
错误回复 vote
错误覆盖 log
```

理论证明不会替 buggy implementation 兜底。

---

# Part 5：从 Happy Path 构造正确的 RPC Pattern

先看一个 election。

```
A             B             C
│             │             │
│ timeout     │             │
│             │             │
│ RV(term=7) ──────────────>│
│ RV(term=7) ─>│             │
│             │             │
│<── yes ─────│             │
│<────────────── yes ───────│
│             │             │
│ Leader      │             │
```

实现上很容易写成：

```
go requestVote(B)
go requestVote(C)
```

但正确结构更像：

```
LOCK
 │
 ├─ check current state
 │
 ├─ increment term
 │
 ├─ become Candidate
 │
 ├─ vote for self
 │
 └─ COPY immutable RPC args
 │
UNLOCK
 │
 │
 ├──── RPC B
 │
 └──── RPC C
       │
       ▼
     response
       │
LOCK
 │
 ├─ does reply belong to current term?
 ├─ am I still Candidate?
 ├─ did peer report higher term?
 └─ only then mutate state
 │
UNLOCK
```

这是整节课可以压缩出的 canonical pattern：

```
Prepare
   ↓
Snapshot
   ↓
Unlock
   ↓
Async operation
   ↓
Lock
   ↓
Revalidate
   ↓
Apply
```

---

# Part 6：时间线——为什么必须 Revalidate

来看完整 example。

初始：

```
A:
term = 7
Candidate
```

时间线：

```
time ─────────────────────────────────────────>

Election G:
  Lock
  term = 7
  role = Candidate
  copy term=7
  Unlock
       │
       └──── RequestVote(B, term=7) ────────>

AppendEntries Handler:
                     receive term=8
                     Lock
                     term = 8
                     role = Follower
                     Unlock

RPC G:
                                           ← yes
                                           Lock
```

现在 RPC goroutine 应该检查：

```
if reply.Term > rf.currentTerm {
    // update term, step down
}

if rf.currentTerm != sentTerm {
    // stale reply
    return
}

if rf.state != Candidate {
    // election no longer active
    return
}
```

核心不是具体 Go 代码。

而是：

```
reply 是否仍然属于
当前正在进行的 state transition？
```

因此常见设计模式叫：

```
version validation
generation validation
epoch validation
term validation
```

Raft 天然已经有：

```
term
```

作为非常重要的 logical epoch。

MIT guidance 也专门指出：RPC reply 返回后，代码必须重新检查 relevant assumptions，例如 term 是否已经从当初开始 election 时发生变化。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)

---

# Part 7：Shared State 和 Invariants

以后写 Raft，你可以把 `Raft` struct 想象成：

```
type Raft struct {
    mu sync.Mutex

    currentTerm int
    votedFor    int
    log         []LogEntry

    state       State

    commitIndex int
    lastApplied int

    nextIndex  []int
    matchIndex []int
}
```

这里不能只问：

```
哪个 variable 要锁？
```

要问：

```
哪些 invariants
横跨这些 variables？
```

几个非常重要的 invariant。

---

### Invariant 1

```
currentTerm 永远不能下降
```

所以：

```
read term
compare term
update term
```

通常需要属于一个 atomic decision。

---

### Invariant 2

如果：

```
state == Candidate
```

那么它的 candidate 身份对应：

```
currentTerm
```

不能出现：

```
term = 9

但是仍在处理
term = 7 election
```

---

### Invariant 3

旧 RPC reply 不能修改新 epoch 的状态。

即：

```
RPC sent at term T

reply returns later

if currentTerm != T:
    reply belongs to old world
```

通常就不能直接执行原本计划。

---

### Invariant 4

```
lastApplied <= commitIndex
```

并且 apply 必须：

```
log order
```

例如：

```
log:
1 A
2 B
3 C
```

不能：

```
apply 1
apply 3
apply 2
```

MIT 建议专门使用一个 long-running goroutine 来按照顺序把 committed entries 发送到 `applyCh`，因为 `applyCh` 可能 block，而且多个 goroutine apply 会让 ordering 很难保证。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt)

---

# Part 8：Condition Variable 为什么出现

考虑：

```
commitIndex = 10
lastApplied = 7
```

apply goroutine 要做：

```
8
9
10
```

如果：

```
commitIndex == lastApplied
```

怎么办？

Naive：

```
for {
    if commitIndex > lastApplied {
        apply()
    }
}
```

这是：

```
busy spin
```

浪费 CPU。

另一个 naive：

```
time.Sleep(10 * time.Millisecond)
```

可以，但增加：

```
latency
```

更自然：

```
commit goroutine:
    commitIndex changes
           ↓
        Signal

apply goroutine:
        Wait
           ↓
        wake up
           ↓
      check condition
```

也就是：

```
sync.Cond
```

Mental Model：

```
Mutex
= protects invariant

Cond
= sleep until invariant may have become actionable
```

注意：

```
Signal
```

不代表：

```
条件现在一定成立
```

所以应该：

```
for conditionIsFalse() {
    cond.Wait()
}
```

而不是：

```
if conditionIsFalse() {
    cond.Wait()
}
```

因为醒来后：

```
state 可能又改变了
```

---

# Part 9：Mutex vs Channel

Go 初学者常听一句：

> Do not communicate by sharing memory; share memory by communicating.

于是可能想：

```
Raft 全部用 channel
```

但 MIT 的 Raft structure advice 明确说，从实践经验看，Raft 通常用：

```
shared data + locks
```

最 straightforward。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt)

为什么？

因为 Raft 本质上维护大量紧密关联状态：

```
term
role
log
commitIndex
matchIndex
nextIndex
```

很多 transition 是：

```
read several fields
↓
check invariant
↓
modify several fields
```

mutex 非常自然。

---

Channel 更适合：

```
ownership transfer

event notification

pipeline

producer → consumer
```

例如：

```
Raft
  │
  │ committed command
  ▼
applyCh
  │
  ▼
KV state machine
```

这就是很自然的 channel boundary。

所以：

```
Mutex
→ protect shared state

Channel
→ communicate event/data between components
```

并不是二选一。

---

# Part 10：一个非常重要的反例——持锁 RPC

Naive implementation：

```
rf.mu.Lock()

reply := call(peer)

process(reply)

rf.mu.Unlock()
```

看起来：

```
“这样整个 operation 都 atomic！”
```

但这是非常危险的。

假设：

```
A                    B

A.mu.Lock()

RequestVote ------->

                     B.mu.Lock()

                     AppendEntries -> A

                                     handler:
                                     A.mu.Lock()
                                     BLOCK

                     waiting...

A waiting B reply...
```

形成 cycle：

```
A waits B
↑       ↓
└───────┘
```

这就是典型：

```
distributed deadlock
+
local mutex deadlock
```

---

# Part 11：另一个陷阱——Unlock 后才创建 RPC args

看：

```
rf.mu.Lock()

rf.currentTerm++
rf.state = Candidate

rf.mu.Unlock()

go func() {
    rf.mu.Lock()
    args.Term = rf.currentTerm
    rf.mu.Unlock()

    call(args)
}()
```

乍一看有锁。

但错在哪里？

假设 goroutine 很晚才获得 CPU：

```
main:
term 7
become candidate

        ↓

term 8
become follower

        ↓

term 9
become candidate

        ↓

goroutine finally runs

args.Term = 9
```

但这个 goroutine 本来属于：

```
term 7 election
```

现在却发送：

```
RequestVote(term=9)
```

所以正确 mental model 是：

> **RPC args 应当表示“创建这个 operation 时的世界”，而不是“RPC goroutine 终于运行时的世界”。**

因此：

```
mu.Lock()

term := currentTerm
args := RequestVoteArgs{
    Term: term,
    ...
}

mu.Unlock()

go send(args)
```

MIT locking advice 恰好专门指出这种 bug，并建议在持锁时复制当前 term，然后让 goroutine 使用这个 snapshot。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)

---

# Part 12：Raft implementation 其实是 Event-Driven State Machine

这是你应该建立的更深 Mental Model。

不要把 Raft 想成：

```
main() {
    runRaft()
}
```

而应该想成：

```
                   ┌── RequestVote
                   │
                   ├── AppendEntries
                   │
                   ├── RPC reply
                   │
                   ├── election timeout
Raft state <───────┼── heartbeat timer
                   │
                   ├── client Start()
                   │
                   └── shutdown
```

这些都是：

```
events
```

它们触发：

```
state transitions
```

所以逻辑结构：

```
Event
  ↓
Acquire Lock
  ↓
Inspect Current State
  ↓
Validate Event
  ↓
Perform Transition
  ↓
Maintain Invariants
  ↓
Release Lock
```

Raft 就是：

> **一个由 distributed events 驱动的 concurrent finite-state machine。**

---

# Part 13：Safety 和 Liveness

这节课本身没有独立 consensus proof，但它直接决定 Raft proof 的 implementation assumption 是否成立。

### Safety

最重要的问题：

> 什么事情永远不能发生？

从 concurrency implementation 角度：

```
currentTerm 不能倒退

旧 election reply 不能让新 term 成 Leader

同一 logical transition 不能被并发操作撕裂

log apply 顺序不能错

shared state 不能出现不可能组合
```

lock + revalidation 主要服务于：

```
Safety
```

---

### Liveness

另一个问题：

> 正确事情最终能不能发生？

例如：

```
heartbeat goroutine
被一个长期持锁 RPC 卡住
```

即使 safety 可能没坏：

```
系统也可能无法 progress
```

所以：

```
不持锁 blocking
```

主要不仅是 performance 问题，也涉及：

```
liveness
```

---

所以：

```
Safety:
“不做错事”

Liveness:
“最终还能做事”
```

一个程序可以：

```
非常安全：
什么都不做
```

却完全没有：

```
liveness
```

---

# Part 14：Failure Matrix

|情况|主要风险|正确处理思路|
|---|---|---|
|RPC 很慢|reply 属于旧 state|snapshot + revalidate|
|RPC 丢失|goroutine 长时间等待 / retry|不持 mutex 等 RPC|
|RPC reply 延迟|stale reply 修改新 term|检查 term/state|
|RPC reorder|较旧 reply 覆盖较新状态|version/term validation|
|两个 handlers 并发|shared state inconsistent|mutex|
|goroutine 同时更新 term|term 倒退|atomic check+update|
|channel block|持锁导致整个 Raft 卡住|unlock before blocking|
|peer unreachable|一个 peer 拖住多数派|concurrent RPCs|
|election timer 并发|多次 election 相互污染|epoch/term checking|
|apply blocking|commit path 被阻塞|dedicated apply goroutine|
|node crash|memory state 消失|后面的 Raft persistence|
|network partition|旧 leader 可能仍认为自己 leader|Raft quorum/term；不是 mutex 能解决|

注意最后两个：

```
local concurrency mechanism
```

不能处理：

```
distributed failure
```

职责边界非常重要。

---

# Part 15：为什么每个 RPC 通常单独 goroutine

假设：

```
Leader A

├─ B fast
├─ C fast
├─ D DEAD
└─ E slow
```

如果 sequential：

```
send B
wait

send C
wait

send D
........ forever
```

后面根本走不动。

而：

```
go replicate(B)
go replicate(C)
go replicate(D)
go replicate(E)
```

那么：

```
B ACK
C ACK

A+B+C = majority
```

不需要等待 D。

这体现 distributed systems 一个非常重要原则：

> **不要让一个慢节点阻塞整个 quorum progress。**

官方 Raft structure advice 也建议 RPC 单独 goroutine，这样 unreachable peer 不会拖延多数 reply，而且 heartbeat/election timers 可以继续运行。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt)

这跟你熟悉的：

```
fan-out RPC
hedged request
quorum query
parallel cloud API calls
```

本质上非常接近。

---

# Part 16：Timer 为什么特别难

Raft 需要：

```
Follower:
没有及时收到 heartbeat
         ↓
election timeout
         ↓
Candidate
```

同时：

```
Leader:
periodically heartbeat
```

于是：

```
Timer
+
RPC
+
state changes
+
goroutine scheduling
```

叠在一起。

比如：

```
time →

timer:   -------- timeout!
                     |
RPC:         heartbeat arrives
                     |
handler:             reset logical timeout?
                     |
timer G:             start election?
```

如果你处理不当：

```
heartbeat 已经收到

但旧 timer event
仍然触发 election
```

于是出现无意义的：

```
term churn
```

MIT 的结构建议倾向于为 heartbeat/election 分别使用 long-running goroutine；对于 election timeout，建议维护“最近听到 leader 的时间”，周期性检查，而不是构造复杂 Timer reset choreography。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt)

这里背后的设计原则比具体 API 更重要：

> **尽量把 concurrent state machine 转化成简单、可检查的状态，而不是复杂 callback/timer interaction。**

---

# Part 17：Race Detector 能解决什么，不能解决什么

你写：

```
go test -race
```

它能找到类似：

```
G1:
currentTerm++

G2:
read currentTerm
```

没有 synchronization。

非常有用。

但它发现不了：

```
mu.Lock()
term := currentTerm
mu.Unlock()

reply := rpc()

mu.Lock()

// perfectly synchronized
state = Leader

mu.Unlock()
```

即使：

```
term 已经 changed
```

因为内存同步完全正确。

只是算法逻辑错误。

所以 debugging 分成：

```
Level 1
Memory Safety / Data Race
    ↓
-race

Level 2
Protocol State Invariants
    ↓
structured logs
assertions

Level 3
Distributed Execution
    ↓
timeline reconstruction
```

官方 Lab debugging advice 也指出，Raft 中不同 peer 与同一 peer 内多个 goroutine 的动作会发生意料之外的 interleaving，比如一个 leader 发 RPC 后，在 reply 回来之前已经丢掉 leadership；建议用日志重建发生了什么。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html)

---

# Part 18：最推荐的 Raft Debug Log

不要只打印：

```
send RequestVote
received reply
```

这几乎没用。

打印：

```
time
node
term
role
event
peer
log index
commitIndex
```

例如：

```
12.031 A T7 Candidate START_ELECTION
12.033 A T7 Candidate SEND_RV B
12.034 A T7 Candidate SEND_RV C

12.050 A T8 Follower RECV_AE C

12.090 A T8 Follower RECV_RV_REPLY B sentTerm=7 IGNORE_STALE
```

你真正想看到的是：

```
state transition history
```

而不是：

```
printf history
```

---

# Part 19：Assertions 非常重要

例如你认为：

```
commitIndex >= lastApplied
```

可以：

```
if rf.lastApplied > rf.commitIndex {
    panic("invariant violated")
}
```

或者：

```
currentTerm never decreases
```

保留：

```
oldTerm
```

检查 transition。

MIT Lab guidance 也建议在代码里加入对 assumptions 的显式检查，使违反 invariant 的问题尽早暴露。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html)

Distributed debugging 中：

```
crash immediately at invariant violation
```

往往比：

```
20 秒以后 test timeout
```

容易调一百倍。

---

# Part 20：Top 5 Misconceptions

### ❌ 1. 加 mutex 就线程安全了

只能说：

```
没有明显 data race
```

不能说明：

```
protocol logically correct
```

关键仍然是：

```
invariants
```

---

### ❌ 2. RPC 返回时，我发送 RPC 时的条件仍然成立

这是本 Lecture 最大错误。

正确：

```
Send:
snapshot assumptions

Receive:
revalidate assumptions
```

---

### ❌ 3. Lock 越大越安全

Coarse-grained lock 确实通常更容易推理。

但：

```
Lock
↓
RPC
↓
wait
```

会导致：

```
deadlock
liveness failure
poor responsiveness
```

正确原则：

```
critical section 尽量覆盖完整 state transition

但不要跨 blocking boundary
```

---

### ❌ 4. Channel 比 mutex 更高级

不是。

它们解决不同结构：

```
mutex
→ shared-state synchronization

channel
→ communication / handoff
```

Raft 状态高度 interconnected，所以 mutex 往往更自然。

---

### ❌ 5. 一个 Raft node 是 sequential state machine，所以内部不用担心 concurrency

理论模型可以把：

```
Raft node
```

描述成一个 state machine。

Implementation 却是：

```
many concurrent goroutines
        ↓
must collectively behave
as if they implement
the state-transition rules correctly
```

这是理论模型和工程实现的关键边界。

---

# Part 21：和 Kubernetes Controller 联系

这里有一个非常好的类比，但要注意它只是 mental model。

Controller：

```
Desired State

      ↓

Observe

      ↓

Reconcile

      ↓

External API Call

      ↓

Observe Again
```

例如：

```
Controller reads:

Generation = 10
desired replicas = 3
```

然后调用 Cloud API：

```
CreateNodePool()
```

调用完成可能已经过去 10 秒。

这期间：

```
Generation:
10 → 11

desired replicas:
3 → 0
```

如果 Controller 回来直接：

```
“创建成功，现在继续按 replicas=3 执行”
```

就和 Raft stale RPC reply 问题非常像。

正确思路是：

```
Observe
↓
derive action from version X
↓
external operation
↓
re-observe/revalidate
```

Kubernetes 里的：

```
resourceVersion
generation
UID
```

在很多场景都起到：

```
“我看到的还是不是同一个世界？”
```

的作用。

但别混淆：

```
Raft term
```

具有 consensus protocol 中严格定义的 epoch semantics；

```
resourceVersion
```

不是 Raft term。

这里只是在说明：

> **跨异步边界后不要相信旧 observation。**

---

# Part 22：和 Cloud Control Plane 联系

这跟你的 infra/controller 场景尤其接近。

例如：

```
Reconcile Region

state:
Creating
```

发请求：

```
CreateEKS()
```

20 分钟后回来。

过程中用户可能：

```
Creating
   ↓
Deleting
```

旧 worker 回来：

```
Create succeeded!
state = Ready
```

于是：

```
Deleting → Ready
```

错误。

典型解决方法：

```
operation generation
revision
resource version
fencing token
state transition validation
```

本质就是 Lecture 5：

```
Snapshot

      ↓

Async Boundary

      ↓

Revalidate

      ↓

Commit Result
```

这是远远超出 Raft Lab 的通用工程思想。

---

# Part 23：和 Terraform 联系

Terraform：

```
Read state
   ↓
Plan
   ↓
Apply
```

本质上也存在：

```
Plan based on old world

        ↓

world changes externally

        ↓

apply stale assumption
```

所以 Terraform 有：

```
state locking
refresh
provider read-after-write
drift detection
```

当然它和 Raft 并不等价。

但共同问题是：

> **decision 是基于哪个 version of reality 做出来的？**

---

# Part 24：和 Database Transaction 联系

数据库中：

```
Read x
↓
compute
↓
Write x
```

如果并发 transaction：

```
T1 read x=10
T2 read x=10

T1 write 11
T2 write 11
```

出现 lost update。

解决：

```
lock
MVCC
validation
```

Raft RPC handling：

```
read term=7
↓
network wait
↓
assume term still 7
↓
write state
```

也非常类似 optimistic concurrency：

```
read version
↓
do work
↓
validate version
↓
commit
```

因此可以这样理解：

```
Raft term check
≈
一种 protocol-level optimistic validation
```

这不是严格等价，但 mental model 非常有帮助。

---

# Part 25：Lab 中你真正应该怎么组织代码

不要把这看成标准答案，而是一种容易 reasoning 的 architecture。

```
Raft
│
├── electionLoop
│      periodic check timeout
│
├── heartbeatLoop
│      leader periodic replication
│
├── RequestVote handler
│
├── AppendEntries handler
│
├── per-peer RPC goroutines
│
└── applier
       commitIndex
          ↓
       applyCh
```

共享状态：

```
                rf.mu
                  │
      ┌───────────┼───────────┐
      ▼           ▼           ▼
 currentTerm     log[]       state
 votedFor       commit      nextIndex
```

核心规则：

```
所有 shared protocol state

      ↓

一个明确 locking protocol

      ↓

external wait 不在 critical section 中

      ↓

回来以后 revalidate
```

---

# Part 26：你写 Raft 时的 5 条 Locking Rules

MIT 官方 locking advice 基本可以压缩成：

#### Rule 1

共享数据：

```
multiple goroutines
+
at least one writer
```

使用 synchronization。

#### Rule 2

多个 update 构成一个 atomic state transition：

```
整个 transition 一起锁。
```

#### Rule 3

如果：

```
read
→ decision
→ write
```

中间不能允许状态变化：

```
整个逻辑一起锁。
```

#### Rule 4

一般不要：

```
hold lock
+
wait
```

尤其：

```
RPC
channel
sleep
timer
```

#### Rule 5

任何：

```
unlock
  ↓
wait
  ↓
lock
```

后：

```
重新检查 assumptions。
```

这五条几乎就是本课最核心的工程教材。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)

---

# Part 27：Problem → Solution Chain

把整课压缩成你喜欢的形式：

```
Raft 有很多 concurrent events
        ↓
Naive:
每个 goroutine 直接修改 state
        ↓
Failure:
Data Race
        ↓
Mechanism:
Mutex
        ↓
New Failure:
状态 transition 被拆成多个 critical sections
        ↓
Mechanism:
Protect invariants / atomic transitions
        ↓
New Failure:
持锁 RPC
        ↓
Failure:
Deadlock / blocked progress
        ↓
Mechanism:
Unlock before network wait
        ↓
New Failure:
等待期间 world changes
        ↓
Failure:
Stale RPC reply
        ↓
Mechanism:
Snapshot term/state
+
Revalidate after RPC
        ↓
New Problem:
apply / timer / heartbeat 并发
        ↓
Mechanism:
Dedicated goroutines
+
Cond
+
simple ownership boundaries
        ↓
Final Mental Model:
Concurrent event-driven state machine
```

---

# Part 28：一个统一模板

以后看到任何 distributed async code，都可以套：

```
             ┌──────────────┐
             │ Current State│
             └──────┬───────┘
                    │
                 LOCK
                    │
                    ▼
              Check invariant
                    │
                    ▼
              Make decision
                    │
                    ▼
             Snapshot version
                    │
                 UNLOCK
                    │
                    ▼
        RPC / I/O / channel / wait
                    │
                 LOCK
                    │
                    ▼
             Revalidate version
                    │
             ┌──────┴───────┐
             │              │
          still valid     stale
             │              │
          commit          ignore
             │
          UNLOCK
```

我建议你把这张图记下来。

它不只是：

```
Raft pattern
```

也是：

```
controller pattern
database OCC pattern
async workflow pattern
cloud control-plane pattern
```

---

# Part 29：回答前面那个 Election 问题

原 scenario：

```
① A 在 term=7 发起 election

② RequestVote B/C

③ A 收到 term=8 AppendEntries

④ A:
   term=8
   Follower

⑤ B 返回 term=7 election 的 yes
```

第⑤步至少应该重新验证：

```
1. currentTerm 是否仍然 == sentTerm(7)

2. state 是否仍然 == Candidate

3. reply.Term 是否 > currentTerm
```

这里：

```
currentTerm = 8
sentTerm    = 7
```

所以：

```
reply belongs to stale election
```

直接：

```
ignore
```

不能：

```
voteCount++
```

更不能：

```
become Leader
```

---

# Part 30：为什么 term 是如此重要的 abstraction

你后面学 Raft 时会发现：

```
term
```

不仅是一个数字。

它本质上是：

```
Logical Epoch
```

例如：

```
term 5:
some leadership world

term 6:
next leadership world

term 7:
another leadership world
```

因此 RPC 携带：

```
Term
```

实际上是在说：

> **“这个 message 是哪个 version of the distributed world 产生的？”**

然后 receiver 可以判断：

```
message.term < my.term
        ↓
message from old world
```

这就是为什么 Lecture 5 和 Raft 紧挨着非常合理：

> Raft protocol 中大量 correctness，最终都会落到“异步结果是否仍属于当前 epoch”这个实现问题上。

---

# Part 31：这课和 State Machine Replication 的深层联系

理论上：

```
State Machine

State S
+
Event E
↓
State S'
```

非常干净。

例如：

```
Follower(term 7)
+
ElectionTimeout
↓
Candidate(term 8)
```

但真实 implementation：

```
ElectionTimeout goroutine
AppendEntries goroutine
RequestVote goroutine
RPC reply goroutine
Client goroutine
```

全部可能同时跑。

因此 mutex 等机制的最终使命是：

> **让 concurrent implementation 看起来仍然像在执行合法的 sequential state transitions。**

这是很深的一层理解。

可以画成：

```
Concurrent implementation
        │
        │ synchronization
        ▼
Equivalent legal state transitions
        │
        ▼
Protocol proof applies
```

如果这一步失败：

```
Raft proof
```

和你的程序没有关系。

---

# Part 32：这一课没有解决什么

非常重要。

Lecture 5 解决：

```
single peer 内部 concurrency correctness
```

以及：

```
如何正确处理 asynchronous RPC lifecycle
```

它没有解决：

```
谁应该成为 Leader？

为什么 majority 足够？

为什么两个 leader 不会 commit conflicting entry？

log conflict 怎么修？

crash-recovery 怎么持久化？

怎样保证 Leader Completeness？

怎样 guarantee State Machine Safety？
```

这些才是 Lecture 6/7 Raft 的主题。

所以：

```
Lecture 5:
How to correctly execute protocol rules

Lecture 6/7:
What those protocol rules should be
```

---

# Part 33：30 秒版本

如果面试官问：

> Go, Threads, and Raft 这课主要讲什么？

可以回答：

> 这节课的核心是如何把 Raft 这种异步 distributed protocol 正确实现成 concurrent Go program。一个 Raft peer 有很多并发事件，例如 RPC handlers、RPC replies、election timers、heartbeats 和 apply loop，它们共享 term、role、log 等状态，因此需要用 mutex 保护 protocol invariants，而不仅仅是避免 data race。同时不能持锁等待 RPC，否则容易造成 deadlock 或阻塞 progress；释放锁等待之后，之前的 assumptions 可能已经失效，所以 RPC reply 必须根据 term/state 重新 validation。核心 pattern 就是 **lock → snapshot state → unlock → async operation → lock → revalidate → apply result**。

---

# Part 34：3 分钟版本

整节课可以理解成解决：

```
“distributed concurrency”
+
“local concurrency”
```

叠加的问题。

一个 Raft peer 内部有很多 goroutine：

```
timer
RPC handler
RPC sender
heartbeat
applier
```

它们共同操作：

```
term
state
log
commitIndex
```

因此首先需要：

```
mutex
```

但 mutex 的真正目的不是简单保护变量，而是保护：

```
protocol invariant
```

比如：

```
currentTerm 不能下降
```

以及：

```
term 和 role 的 transition
必须被原子观察
```

但是不能：

```
hold mutex
→ RPC
```

因为 RPC 可能无限延迟，并造成：

```
deadlock/liveness problems
```

所以需要：

```
Lock
snapshot
Unlock
RPC
Lock
```

而这里产生第二个核心问题：

```
RPC 期间 state 可能改变
```

因此返回后必须：

```
revalidate term/state
```

否则旧 RPC reply 可能污染新的 election。

这其实是一种非常通用的：

```
versioned asynchronous computation
```

模式。

---

# Part 35：深入版本

```
Problem
│
├─ Raft peer has concurrent events
│
├─ network is asynchronous
│
└─ shared state is mutable
│
▼
Model
│
├─ many goroutines
├─ shared Raft struct
├─ unreliable/delayed network
└─ asynchronous execution
│
▼
Mechanisms
│
├─ Mutex
│   └─ protect invariants
│
├─ goroutine
│   └─ allow independent progress
│
├─ channel
│   └─ component communication
│
├─ sync.Cond
│   └─ wait for state condition
│
└─ term/version validation
    └─ reject stale async results
│
▼
Core Invariants
│
├─ term never decreases
├─ state transitions are atomic
├─ stale RPC replies don't mutate new epoch
├─ applied entries preserve order
└─ blocking work doesn't freeze peer state
│
▼
Safety
│
└─ concurrent execution cannot produce
   illegal protocol state
│
▼
Liveness
│
├─ unreachable peer doesn't block majority
├─ timers keep running
└─ apply/heartbeat/election make progress
│
▼
Failure Handling
│
├─ delay → revalidation
├─ loss → retry/concurrent RPC
├─ reorder → term/version checks
└─ crash → later Raft persistence machinery
│
▼
Trade-off
│
├─ coarse locks easier to reason about
│   but more contention
│
└─ fine-grained concurrency faster
    but dramatically harder to prove/debug
```

---

# Part 36：知识网络

最终把今天的知识挂在这里：

```
                    Distributed Systems
                           │
                 Asynchronous Execution
                           │
           ┌───────────────┴────────────────┐
           │                                │
   Local Concurrency                 Network Concurrency
           │                                │
  goroutine / mutex                 RPC / delay / loss
  channel / Cond                    reordering / partition
           │                                │
           └───────────────┬────────────────┘
                           │
                      Raft Peer
                           │
                    State Transitions
                           │
              ┌────────────┴─────────────┐
              │                          │
        Synchronization              Epoch / Term
              │                          │
      protect invariants          reject stale work
              │                          │
              └────────────┬─────────────┘
                           │
                          Raft
                           │
                  Consensus / Replication
                           │
                State Machine Replication
                           │
               Fault-Tolerant KV Service
                           │
                  etcd / ZooKeeper etc.
```

---

### 最重要的知识压缩

如果这节课最终只留下 **三个脑回路**，我希望是这三个：

```
① Mutex 保护的不是变量，
   而是 invariant / state transition。
```

```
② Never trust an assumption
   across an asynchronous boundary.

   RPC / unlock / wait 之后，
   世界可能已经完全不同。
```

```
③ Distributed algorithm 的 proof
   假定每个 node 正确执行 protocol。

   所以 local concurrency correctness
   是 distributed correctness 的地基。
```

而最值得你真正内化的一张图，就是：

```
LOCK
  ↓
observe state
  ↓
make decision
  ↓
snapshot term/version
  ↓
UNLOCK
  ↓
RPC / wait
  ↓
LOCK
  ↓
REVALIDATE
  ↓
still valid?
  ├─ YES → apply
  └─ NO  → discard
```

从下一讲开始学 **Raft Leader Election** 时，你会反复看到这张图：`term`、`RequestVote`、`AppendEntries`、election timeout、late RPC reply，几乎所有难点都可以放回这个 Mental Model 中理解。