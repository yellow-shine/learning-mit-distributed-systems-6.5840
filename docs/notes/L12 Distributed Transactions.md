可以。这次我们不把它当成“2PC 复习课”，而是把 **Distributed Transactions 整个问题空间重新搭起来**。

MIT 6.5840/6.824 官方材料确实把这部分的重点放在 **Two-Phase Locking (2PL)** 与 **Distributed Two-Phase Commit (2PC)**，目标是得到 serializable transaction execution；历年考试也反复考 `PREPARED` 状态为什么不能随便回滚。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/questions.html?lec=6&q=q-raft2&utm_source=chatgpt.com)

---

# Part 1：如果只记一个问题

> **一个 transaction 跨越多个独立 shard 时，怎样保证它在并发、crash、message loss、network partition 下，仍然像一个正确的单机 transaction？**

先看最小例子：

```
              Transfer $100
             Alice -> Bob
                   |
          +--------+--------+
          |                 |
          v                 v
       Shard A           Shard B

Alice = 1000           Bob = 500
```

事务：

```
Alice -= 100
Bob   += 100
```

单机数据库当然也有 concurrency、crash recovery、transaction 问题。所以严格来说不是：

> 单机没有 transaction 问题。

而是：

> 单机没有“多个独立 failure domain 必须对同一个 transaction outcome 达成一致”这个额外问题。

单机可以把：

```
lock
WAL
buffer pool
commit record
recovery
```

都放在一个 DBMS 内部协调。

但 distributed system 中：

```
Shard A 可能成功
Shard B 可能失败

Coordinator 可能 crash

ACK 可能丢

网络可能 partition
```

最 naive：

```
write Alice -= 100
write Bob   += 100
```

Happy Path：

```
Alice = 900
Bob   = 600
```

完全正确。

问题是：

```
write Alice -= 100   ✓

Coordinator crashes  X

write Bob += 100     没执行
```

结果：

```
Alice = 900
Bob   = 500
```

钱消失了。

你可能说：

```
retry Bob += 100
```

但如果真实情况其实是：

```
Bob 已经加了 100
只是 ACK 丢了
```

retry 又会导致：

```
Bob = 700
```

所以 Distributed Transactions 的第一个敌人不是单纯的：

> failure

而是：

> **uncertainty：你不知道远端到底执行到哪里了。**

然后即使完全没有 crash，还有第二个问题。

另一个 transaction：

```
T2:
read Alice
read Bob
print Alice + Bob
```

可能看到：

```
T1: Alice -= 100 ---------------- Bob += 100

T2:         read Alice=900
                      read Bob=500
```

于是：

```
900 + 500 = 1400
```

它观察到了一个本来不应该存在的世界。

因此这一 Lecture 实际要解决两个相互独立的问题：

```
                 Distributed Transaction
                         |
             +-----------+-----------+
             |                       |
         Atomicity                 Isolation
             |                       |
     all commit / abort       concurrent txns
             |               look serializable
             |                       |
            2PC                     2PL
```

这是整节课最重要的 Mental Model。

---

# Part 2：它在 6.824 里的位置

把课程压缩成这样：

```
RPC
 |
 | 机器之间如何调用
 v
Failure / Retry
 |
 | RPC 可能只有 "unknown"
 v
Replication
 |
 | 一份 logical state 如何有多个 copies
 v
Consensus / Raft
 |
 | replicas 如何 agree on one history
 v
State Machine Replication
 |
 | 一个 replicated shard 可以正确运行
 v
ZooKeeper / replicated services

------------------------------------------------

现在：

Shard A 是一个正确 replicated system
Shard B 也是一个正确 replicated system

但是：

一个 transaction 同时碰 A + B 怎么办？
                  |
                  v
        Distributed Transactions
          |                 |
         2PL               2PC
          |                 |
   concurrency           atomic commit
          +--------+--------+
                   |
                   v
                Spanner
```

后面的 Spanner 恰恰会把这些机制组合起来：一个 transaction 可以跨多个 replicated Paxos group，需要 transaction-level concurrency control 和 distributed commit。MIT 2026 课程也是 Distributed Transactions 后紧接 Spanner。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

这里必须把几个边界彻底分清。

### Consensus vs 2PC

Consensus 问：

```
A、B、C 三个 replica：

我们最终选择 value X 还是 value Y？
```

例如：

```
Raft Group for Shard A

A1
A2  → agree on Shard A's log
A3
```

2PC 问：

```
Shard A：
我能不能 commit transaction T？

Shard B：
我能不能 commit transaction T？

两边必须得到同一个最终 outcome：
COMMIT / ABORT
```

现实数据库经常是：

```
               Transaction T
                    |
          +---------+---------+
          |                   |
          v                   v
       Shard A              Shard B
          |                   |
      Raft group           Raft group
     A1 A2 A3             B1 B2 B3
```

也就是：

```
Raft / Paxos
= 一个 shard 内 replication

2PC
= 多个 shard 之间 atomic transaction
```

它们不是竞争关系。

经常是上下叠加关系。

---

# Part 3：六个核心 Mental Models

### Concept 1：Transaction Atomicity

#### 它解决什么

不能出现：

```
Shard A committed
Shard B aborted
```

#### 一句话

> Transaction 的所有 participating shards 要么全部 commit，要么全部 abort。

#### 注意

这里的 Atomicity 不是：

```
CPU atomic instruction
```

也不是：

```
linearizability
```

更不是：

```
replication
```

而是：

```
all-or-nothing
```

---

## Concept 2：Serializability

考虑：

```
T1:
x = x + 1
y = y + 1

T2:
read x
read y
```

真实运行可能高度 interleaved。

Serializability 要求：

> 最终效果必须等价于某一种 serial execution。

例如只能像：

```
T1 → T2
```

或者：

```
T2 → T1
```

但不能看到：

```
T2 reads:

x = after T1
y = before T1
```

如果：

```
x=0
y=0
```

T1：

```
x=1
y=1
```

那么 T2 合法观察：

```
0,0
```

或：

```
1,1
```

不能：

```
1,0
```

因为不存在任何 serial ordering 可以产生它。

---

### Serializability vs Linearizability

这是很容易混淆的一组。

#### Serializability

讨论：

```
multiple transactions
```

要求结果：

```
equivalent to some serial ordering
```

但是这个 serial order 未必尊重真实时间。

例如：

```
现实：

T1 completes
             T2 starts
```

普通 Serializability 理论上仍可能选择：

```
T2 → T1
```

作为等价 serial order。

---

#### Strict Serializability

再增加：

```
real-time order
```

如果：

```
T1 完全结束
然后
T2 才开始
```

那么 serial order 必须：

```
T1 → T2
```

这通常叫：

```
Strict Serializability
```

Spanner 论文里的 External Consistency 与这个概念密切对应。现代文献通常描述它为 Serializability 加 real-time ordering。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/osdi23-eldeeb.pdf?utm_source=chatgpt.com)

---

#### Linearizability

经典 Linearizability 更多是在说：

```
individual operations / concurrent object
```

例如：

```
Put(x=1)
Get(x)
```

像是在 operation 执行区间里的某一点瞬间发生。

你可以粗略建立：

```
Linearizability
        |
single operations / objects

Strict Serializability
        |
transactions
```

不要完全把两者看成同一个概念，但它们都加入了 real-time 约束。

---

## Concept 3：Two-Phase Locking

现在先完全忽略 crash。

只解决并发。

初始：

```
Alice=1000
Bob=500
```

T1：

```
Alice -=100
Bob   +=100
```

T2：

```
read Alice
read Bob
```

naive：

```
T1: write Alice=900
T2: read Alice=900
T2: read Bob=500
T1: write Bob=600
```

T2 看到了：

```
900, 500
```

所以我们加 lock。

---

### naive lock

T1：

```
lock Alice
write Alice
unlock Alice

lock Bob
write Bob
unlock Bob
```

看起来合理。

但：

```
time →

T1: lock A
    A=900
    unlock A

T2:          lock A
             read A=900
             unlock A

             lock B
             read B=500

T1:                    lock B
                       B=600
```

仍然错。

问题不在：

```
有没有 lock
```

而在：

> 什么时候允许释放 lock？

---

## Two-Phase Locking

2PL：

```
Phase 1: Growing

只能 acquire locks
不能 release

          ↓

Phase 2: Shrinking

只能 release locks
不能 acquire 新 lock
```

示意：

```
lock A
lock B

---------- lock point ----------

operate A
operate B

unlock A
unlock B
```

一旦开始：

```
unlock
```

之后不能：

```
lock new object
```

MIT 历年考试专门用“读完一个 object 马上 unlock，再去读另一个”作为 non-serializable counterexample。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q23-2-sol.pdf?utm_source=chatgpt.com)

---

## Strict 2PL

实际数据库通常采用更强的变体。

尤其 write lock：

```
until COMMIT / ABORT
```

都不释放。

最容易建立的 mental model 是：

```
BEGIN

lock A
lock B

read/write...

PREPARE
COMMIT

unlock A
unlock B
```

这对 distributed transaction 极其重要。

因为如果：

```
Participant A PREPARED
```

却把 lock 放掉，那么别的 transaction：

```
T2
```

可能读到/修改与 T1 冲突的数据。

之后如果 T1 又 commit，就可能破坏 serializability。

---

## Concept 4：2PC

2PL 解决：

```
并发 transaction 怎么排序
```

但仍没回答：

```
Shard A + Shard B
怎么一起 commit？
```

于是有 2PC。

先看最干净的模型：

```
             Transaction Coordinator
                      TC
                     /  \
                    /    \
                   v      v
                Shard A  Shard B
```

---

# Part 5：从 Happy Path 推导 2PC

Transaction 已经执行完逻辑：

```
T:
A -= 100
B += 100
```

暂时还只是 tentative state：

```
Shard A:
Alice=900, uncommitted
lock Alice held

Shard B:
Bob=600, uncommitted
lock Bob held
```

现在进入 commit protocol。

---

### Phase 1：Prepare

Coordinator：

```
TC → A : PREPARE T
TC → B : PREPARE T
```

participant 必须回答：

```
YES
```

或者：

```
NO
```

但是这个 YES 有非常强的含义。

YES 不是：

> “现在看起来应该可以。”

而是：

> **“从现在开始，即使我 crash/reboot，我仍保证如果你之后让我 COMMIT，我一定能完成 commit。”**

所以 YES 前 participant 通常必须：

```
1. transaction validation 通过
2. 所需 lock 已持有
3. transaction state 写入 durable storage
4. PREPARED record durable
```

然后：

```
reply YES
```

状态：

```
INIT
 |
 v
PREPARED
```

这一步极其重要。

---

## 为什么需要 PREPARED state

假设 A：

```
PREPARE
durably log PREPARED
YES
```

然后立即：

```
crash
```

重启后必须知道：

```
我曾经答应过 YES。
```

否则它可能说：

```
不知道这个 transaction
那我 abort 吧
```

但 Coordinator 可能已经：

```
收到 A YES
收到 B YES

决定 COMMIT
```

甚至 B 已经向其他 transaction 暴露 committed result。

此时 A 再自行 abort 就会产生：

```
A abort
B commit
```

MIT 的考试就专门考过这个错误：`PREPARED` participant crash 后不能直接忘记 transaction，因为 TC 可能已经决定 COMMIT。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q24-1-sol.pdf?utm_source=chatgpt.com)

---

## Phase 2：Decision

如果：

```
A YES
B YES
```

Coordinator 可以：

```
COMMIT
```

如果：

```
任何一个 NO
```

则：

```
ABORT
```

所以：

```
                all YES
Prepare ----------------------> COMMIT

             any NO
Prepare ----------------------> ABORT
```

关键：

Coordinator 在发送：

```
COMMIT
```

之前，要先 durable log：

```
DECISION = COMMIT
```

为什么？

我们马上 crash。

---

## Failure #1：TC 发给 A COMMIT 后 crash

```
              TC
              |
              | COMMIT
              v
              A

             X crash

              B
        没收到 COMMIT
```

现在：

```
A = committed
B = prepared
```

如果 TC reboot 后说：

```
呃，我忘了刚才决定什么。
那 abort 吧。
```

直接破坏 atomicity。

因此：

```
TC:

log COMMIT durably
       ↓
send COMMIT
```

重启：

```
read log

DECISION=COMMIT

resend COMMIT to everyone
```

官方历年 2PC 题的标准要求正是如此：TC 必须先从 durable state 恢复 decision，然后把 COMMIT 继续发送给其他 participants。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q18-1-sol.pdf?utm_source=chatgpt.com)

---

## Phase 2 完整 Happy Path

```
time ↓


Coordinator           A                 B

 PREPARE ------------>

 PREPARE -------------------------------->

                      durable PREPARED
                      hold locks
          <---------- YES

                                        durable PREPARED
                                        hold locks
          <----------------------------- YES


durable COMMIT


 COMMIT  ------------>

 COMMIT  -------------------------------->

                      apply/commit
                      release locks

                                        apply/commit
                                        release locks
```

最终：

```
Alice=900
Bob=600
```

---

# Part 6：把时间线中的状态看清楚

这是这节课真正关键的地方。

考虑：

```
A has voted YES
B has voted YES

但 TC 尚未决定
```

此时：

```
A:
PREPARED

B:
PREPARED
```

谁知道什么？

```
A 知道：
我自己能够 commit

A 不知道：
B 是否 YES

A 不知道：
TC 最后会 COMMIT 还是 ABORT
```

同样 B 也不知道。

所以 PREPARED participant 处于一个非常尴尬的位置：

```
不能 commit
因为不知道 global decision

不能 abort
因为 TC 可能已经决定 commit
```

于是它只能：

```
WAIT
```

这就是：

> **2PC 的 blocking property。**

---

## Failure #2：Coordinator 在所有 YES 后 crash

```
TC:
PREPARE A
PREPARE B

A -> YES
B -> YES

TC crashes
```

A、B：

```
PREPARED
```

但不知道：

```
COMMIT?
ABORT?
```

它们不能自行决定。

这是 2PC 最重要的 weakness。

---

## 为什么不能 timeout 后 abort？

这是很多工程师第一反应：

```
等 30 秒。

Coordinator 没回来？

那 abort。
```

错误。

考虑：

```
TC receives all YES
TC durable log COMMIT

TC sends COMMIT to B

TC crashes before COMMIT reaches A
```

现在：

```
B = committed

A = prepared
```

A timeout：

```
"那我 abort"
```

结果：

```
A ABORT
B COMMIT
```

Atomicity 直接炸掉。

所以：

> timeout 可以帮助 liveness decision，但不能凭空创造 safety information。

这和你学 Failure Detector 时的思想是一样的：

```
timeout
≠
证明对方死了

timeout
=
我暂时听不到对方
```

---

# Part 7：Protocol State

Participant 可以简化成：

```
             PREPARE
INIT ----------------------> PREPARED
 |                              |
 | NO                           | COMMIT
 v                              v
ABORTED                     COMMITTED
                               
PREPARED
   |
   | ABORT
   v
ABORTED
```

重要状态：

```
transaction ID
read/write set
locks
undo/redo information
PREPARED record
decision if learned
```

---

### 哪些必须 durable？

#### Participant

如果已经发送：

```
YES
```

必须 durable 保存：

```
PREPARED
+ enough information to commit/abort
```

否则：

```
YES
crash
forget
```

就违约。

---

#### Coordinator

在向任何 participant 发 COMMIT 前：

```
decision = COMMIT
```

必须 durable。

否则：

```
send COMMIT A
crash
forget decision
```

无法恢复。

---

## 最核心 invariant

### Invariant 1

```
Coordinator 只有收到所有 YES
才可以 decide COMMIT。
```

---

### Invariant 2

```
participant 一旦 YES，
不能自行改变成 ABORT。
```

除非知道 global decision 是 ABORT。

---

### Invariant 3

```
Coordinator 一旦决定 COMMIT，
decision 永远不能改变。
```

---

### Invariant 4

在 strict 2PL 下：

```
transaction 的冲突 locks
不能在 global decision 前过早释放。
```

否则 concurrent transaction 可以钻进 prepared transaction 的中间状态。

---

# Part 8：为什么 2PL 保证 Serializability？

这是很多课只说结论、不解释的地方。

考虑 conflicting transactions：

```
T1 needs lock X
T2 needs lock X
```

它们不可能同时持有 exclusive lock。

所以一定存在 order：

```
T1 lock X first
```

那么 T2：

```
wait
```

Two-Phase Locking 的关键性质是：

> 一个 transaction 开始释放 lock 之后，就不会再获取新的 lock。

定义每个 transaction 的：

```
lock point
```

即：

> 它取得最后一个 lock 的时刻。

例如：

```
T1:

lock A
lock B     <- lock point
read/write
unlock A
unlock B
```

然后可以按照：

```
lock point 时间
```

给 transaction 排序。

由于 conflicting transaction 的 lock acquisitions 不能穿插形成不可调和的环，这个顺序构成一个与实际 execution conflict-equivalent 的 serial order。

所以：

```
2PL
→ conflict serializability
```

但注意：

```
2PL ≠ deadlock-free
```

反而非常容易 deadlock。

---

## Deadlock

```
T1:
lock A
        waits B

T2:
lock B
        waits A
```

形成：

```
T1 -> B -> T2 -> A -> T1
```

更直观：

```
T1 holds A
T2 holds B

T1 waits B
T2 waits A
```

谁都走不了。

解决方法可能包括：

```
deadlock detection
wait-for graph

timeout

global lock ordering

wait-die / wound-wait

abort one transaction
```

但：

> deadlock handling 和 serializability 是两个不同问题。

2PL 保证 correctness，却可能牺牲 liveness。

这也是后面你学习 **Optimistic Concurrency Control** 时的重要背景：

```
2PL:
先阻止冲突

OCC:
先跑，最后检测冲突
```

---

# Part 8.5：2PL + 2PC 怎样拼起来

这是这节课最重要的一张图：

```
                 Transaction T

                       |
            execute reads/writes
                       |
                       v
             acquire/hold locks
                    [2PL]
                       |
                       v
                PREPARE phase
                    [2PC]
                       |
                  all YES?
                 /       \
               no         yes
               |           |
             ABORT       COMMIT
               |           |
               +-----+-----+
                     |
               release locks
```

所以：

```
2PL
负责：
谁可以同时访问什么

2PC
负责：
大家最终 commit 还是 abort
```

2PC 本身**不保证 Serializability**。

比如不用 concurrency control：

```
T1 reads x=0
T2 reads x=0

T1 writes x=1
T2 writes x=1

both 2PC successfully commit
```

两个事务都 atomic commit 了。

但一个 increment 丢了。

所以：

```
2PC successful
```

不能推出：

```
execution serializable
```

这一点非常重要。

---

# Part 8.6：反过来也一样

2PL 也不保证 distributed atomic commit。

你可以有完美的 lock：

```
A lock Alice
B lock Bob
```

但是：

```
A commits
Coordinator crashes
B never commits
```

依然违反 atomicity。

所以：

```
            correctness

     Isolation         Atomicity
         |                 |
        2PL               2PC
         |                 |
         +--------+--------+
                  |
          Distributed TX
```

---

# Part 9：Failure Matrix

|Failure|会怎样|Safety|Availability|关键机制|
|---|---|---|---|---|
|Participant crash before YES|TC 可最终 abort|保持|transaction 可能延迟|timeout + abort|
|Participant crash after YES|reboot 后回到 PREPARED|保持|可能 blocking|durable prepare|
|TC crash before decision|prepared participants 等待|保持|可能停止|TC recovery|
|TC crash after COMMIT decision|reboot 后重发 COMMIT|保持|暂时停止|durable decision|
|COMMIT packet loss|participant 保持 PREPARED|保持|暂时阻塞|retry|
|Duplicate COMMIT|重复处理|应保持|通常可用|idempotency|
|Network partition|prepared participants 可能卡住|保持|降低|等网络恢复|
|delayed old message|根据 txn state 忽略/重复执行|可保持|通常可用|transaction ID/state machine|
|disk state 永久丢失|可能无法恢复 promise|2PC 本身无法保证|—|replicated durable storage|
|Byzantine participant|可说谎/违约|不保证|不保证|超出模型|

这张表里最重要的模式：

```
2PC 经常选择：

Safety over Availability
```

network partition 时，它宁可：

```
不做决定
```

也不能：

```
双方猜不同答案
```

---

# Part 10：System Model

现在正式写出这套经典协议通常假设什么。

### Node Model

通常：

```
crash-recovery
```

不是 Byzantine。

节点可以：

```
crash
restart
```

而且依赖 stable storage 恢复重要 protocol state。

participant 不会：

```
恶意撒谎
伪造 vote
故意 violate protocol
```

---

### Network

允许：

```
message loss
message delay
message duplication
message reordering
network partition
```

所以 RPC 需要：

```
retry
transaction ID
idempotent state transition
```

---

### Timing

核心 correctness 不依赖一个可靠的 fixed message latency bound。

更接近：

```
asynchronous model
```

timeout 只能用于：

```
suspect
retry
abort before commit decision
```

不能作为：

```
proof
```

---

### Storage

关键状态必须进入：

```
persistent storage
```

现实中 persistent storage 自己也可能是：

```
replicated log
```

例如某 shard 本身由 Raft/Paxos replicated。

所以：

```
participant PREPARED
```

不一定意味着：

```
写了一块本地 SSD
```

可能意味着：

```
transaction prepare entry
已经 replicated 到 shard quorum
```

---

## Failure Assumption

经典 2PC 能很好应对：

```
temporary crash
temporary network failure
message retry
participant restart
coordinator restart
```

前提是：

```
durable state 最终还能恢复
```

如果：

```
Coordinator 永久消失
并且它是唯一拥有 global decision 的地方
```

prepared participants 可能永远 blocking。

如果：

```
disk 永久丢失
```

又没有 replication：

```
2PC 本身无法创造丢失的数据。
```

如果：

```
Byzantine
```

经典 2PC safety 也不成立。

---

# Part 11：一个完整 Transaction Execution

现在把 2PL + 2PC 连起来。

Transaction：

```
T1:

Alice -= 100
Bob   += 100
```

---

### Step 1：获得 locks

```
Shard A:
X-lock Alice

Shard B:
X-lock Bob
```

---

### Step 2：执行 tentative writes

```
A:
Alice 1000 -> 900
not committed

B:
Bob 500 -> 600
not committed
```

其他 transaction 不能看到不合法的状态。

---

### Step 3：PREPARE

```
TC -> A PREPARE
TC -> B PREPARE
```

A：

```
validate
write PREPARED
retain lock Alice
YES
```

B：

```
validate
write PREPARED
retain lock Bob
YES
```

---

### Step 4：Coordinator decides

全部 YES：

```
TC durable:

T1 = COMMIT
```

这里可以把它看成：

> 整个 transaction 的 point of no return。

一旦 durable COMMIT：

```
T1 不再允许 globally abort
```

即使某 participant 一时没收到消息。

---

### Step 5：broadcast

```
TC -> A COMMIT
TC -> B COMMIT
```

---

### Step 6：participants finalize

```
A commit local state
release Alice lock

B commit local state
release Bob lock
```

---

# Part 12：一个很关键的问题

什么时候 transaction 算：

```
committed
```

要区分三件事：

```
1. participant prepared

2. global commit decision durable

3. 所有 participant 都完成 local commit
```

不是同一个时刻。

例如：

```
TC durable COMMIT

A received COMMIT
B hasn't
```

global outcome 已经是：

```
COMMIT
```

但 B：

```
仍处于 PREPARED
```

它未来必须追上。

这种思想你应该和 Raft 联系起来：

```
logical committed state
```

和：

```
所有 replicas 已经 physical applied
```

也不是一回事。

---

# Part 13：2PC 为什么不是 Consensus？

这是本课必懂。

表面很像：

```
多个机器
最后 agree on COMMIT/ABORT
```

但它们的 problem model 不一样。

---

### 2PC

规则：

```
如果有人 NO
→ ABORT

如果全部 YES
→ coordinator 可以 COMMIT
```

特别依赖：

```
transaction coordinator
```

Coordinator failure 可以使：

```
prepared participants block
```

---

### Consensus

例如 Raft：

```
5 replicas
```

不要求：

```
5/5 都活着
```

而是：

```
3/5 majority
```

仍可决定 log entries。

例如：

```
N = 5
f = 2
```

因为：

```
2f + 1 = 5
```

两个 majority 都至少相交一个节点。

---

### 更正确的组合图

现实 distributed DB 更像：

```
          Distributed Transaction

                   TC

           /                \
          /                  \
         v                    v

     Shard A               Shard B
   Raft/Paxos A           Raft/Paxos B

   A1 A2 A3               B1 B2 B3
```

每个 participant：

```
本身是一个 replicated state machine
```

因此 participant crash 不等于 shard 不可用。

2PC 参与者实际上可能是：

```
Shard A's replicated group
```

而不是：

```
一台 server
```

这是现代 distributed database 最重要的架构模式之一。

---

# Part 14：Blocking 为什么这么难消除

你可能会想：

> Coordinator 也用 Raft replication 不就好了？

这确实大幅改善。

例如：

```
Coordinator state:

COMMIT T1
```

写进一个 replicated consensus group。

这样单节点 coordinator crash：

```
leader failover
```

后仍然能恢复。

因此工程系统通常不会真的让：

```
唯一 coordinator process
+ 唯一 local disk
```

成为严重 single point of failure。

但注意概念边界：

```
replicating coordinator
```

不是改变了 2PC atomic commit semantics。

而是：

```
降低 coordinator state permanent unavailability
```

---

# Part 15：Prepare 的真正含义

如果你只从这课记一个协议状态，我建议记：

> **PREPARED = 我已经失去了自行决定的自由。**

在 PREPARED 之前：

```
participant:
"I can still abort."
```

PREPARED 后：

```
participant:
"I have promised I can commit,
but I don't yet know whether I should commit."
```

这是 2PC blocking 的根源。

你甚至可以把它看成一个 distributed promise：

```
YES vote
=
durable promise
```

这比“第一阶段投票”更接近本质。

---

# Part 16：为什么 lock 必须跟着 PREPARED 状态留下来

假设：

```
T1 locks x
T1 modifies x
T1 PREPARED

然后 unlock x
```

T2：

```
lock x
modify x
commit
```

现在 TC 告诉 T1：

```
COMMIT
```

你可能已经没有一种简单方式把 T1 插回一个正确 serial order。

因此经典：

```
strict 2PL + 2PC
```

会导致一个性能问题：

```
TC unavailable
        ↓
participant PREPARED
        ↓
locks cannot release
        ↓
other transactions block
        ↓
contention spreads
```

这就是为什么 distributed commit latency 会直接放大：

```
lock hold time
```

进而影响 throughput。

---

# Part 17：2PC 的 cost

一个跨 shard transaction，相比 local transaction，多出：

```
Execute
   ↓
PREPARE round
   ↓
durable log writes
   ↓
COMMIT round
```

粗略：

```
Coordinator
    |
    | PREPARE
    |--------------> participants
    |
    |<-------------- YES
    |
    | COMMIT
    |--------------> participants
```

至少涉及额外 network round trips 和 durable state transitions。

现代论文仍然把经典 2PC 的主要 overhead 描述为网络 round trip、persistent logging 和 concurrency coordination。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/osdi23-eldeeb.pdf?utm_source=chatgpt.com)

所以工程设计很自然地尝试：

```
single-shard fast path

coordinator co-location

parallel prepare

replicated WAL optimization

one-phase commit when possible

OCC

timestamp-based CC

MVCC

deterministic transaction execution
```

---

# Part 18：Top 5 Misconceptions

### ❌ 1. 2PC 保证 Serializability

错。

2PC：

```
atomic commit
```

不是：

```
concurrency control
```

你还需要：

```
2PL
OCC
MVCC / timestamp protocol
...
```

---

## ❌ 2. participant timeout 后可以自己 abort

如果它已经：

```
YES / PREPARED
```

不可以。

因为：

```
TC 可能已经 durable COMMIT
```

甚至其他 participant 已经 commit。

---

## ❌ 3. PREPARE = COMMIT

不是。

```
PREPARED:
can commit

COMMITTED:
must/has commit
```

PREPARED 最尴尬：

```
已经 promise
但不知道 global result
```

---

## ❌ 4. Raft 可以替代 2PC

通常不能这么说。

Raft：

```
replicate one logical state machine
```

2PC：

```
atomic transaction across multiple independent participants
```

真实系统：

```
Raft + 2PC
```

经常同时出现。

---

## ❌ 5. Distributed transaction 最难的是 rollback

不是。

真正难的是：

```
某些 participant 已经把结果向世界暴露
```

以后就不能简单：

```
"大家回滚一下吧"
```

因为其他 transaction 已经基于这个 committed state 执行了。

这也是为什么：

```
commit decision
```

存在真正的不可逆边界。

---

# Part 19：Safety / Liveness

### Safety

永远不能发生：

```
Transaction T:

Shard A commits
Shard B aborts
```

以及 concurrency control 目标：

```
committed execution
不能产生 non-serializable history
```

---

#### 2PC Safety 来自

```
all YES before COMMIT

durable PREPARED promise

durable coordinator decision

participants obey decision

idempotent retry
```

---

#### 2PL Safety 来自

```
conflicting operations
受到 locks 排序

acquire/release 满足 two-phase rule
```

因此得到 conflict serializability。

---

## Liveness

希望：

```
transaction eventually commits or aborts
```

但经典 2PC 不无条件保证。

例如：

```
A PREPARED

TC permanently unreachable
```

A：

```
不能 commit
不能 abort
```

于是：

```
blocking forever
```

所以：

```
2PC Safety
```

通常比：

```
2PC Availability
```

更强。

这是典型：

```
network partition

Safety survives
Liveness suffers
```

---

# Part 20：和 Fault Tolerance / Raft 串起来

你之前学的 Failure Detector 在这里特别有价值。

假设：

```
TC timeout participant A
```

这并不表示：

```
A crashed
```

可能：

```
A alive
network delayed
TC partitioned
```

所以 timeout 的语义只能是：

```
"I cannot currently obtain A's vote"
```

而不是：

```
"A has definitely failed"
```

如果当前还没有 commit decision：

```
missing YES
```

Coordinator 可以安全选择：

```
ABORT
```

因为 COMMIT 的必要条件：

```
all YES
```

没有满足。

但是 participant 已经 PREPARED 后：

```
timeout coordinator
```

不能得出：

```
ABORT
```

因为 global COMMIT 可能已经存在。

这是一个很漂亮的 asymmetry：

```
Coordinator before commit decision:
timeout can lead to ABORT

Prepared participant:
timeout cannot independently lead to ABORT
```

---

# Part 21：与 ZooKeeper 的关系

ZooKeeper 可以提供：

```
coordination metadata
leader election
locks
configuration
```

但：

```
ZooKeeper lock
```

并不会自动让：

```
MySQL shard A
MySQL shard B
```

变成一个 atomic distributed transaction。

例如：

```
lock("/transaction/123")

write DB A
write DB B

unlock
```

仍然存在：

```
write A succeeded
crash
write B never happened
```

所以：

```
Distributed Lock
≠
Distributed Transaction
```

这是很常见的工程误解。

---

# Part 22：与 Kubernetes Controller 联系

这里有一个很好的对照，但不能强行等价。

Kubernetes controller：

```
Desired State
     |
     v
Observe
     |
     v
Reconcile
     |
     v
Actual State
```

假设一个 controller 要：

```
create cloud LB
create DNS record
update status
```

这其实也是：

```
跨多个独立系统的 multi-step workflow
```

但是 Kubernetes control-plane 通常**不会**给这些 external side effects 提供传统 ACID distributed transaction。

更多是：

```
eventual reconciliation
+
idempotency
+
retry
+
state machine
+
compensation
```

所以：

```
DB Distributed Transaction

目标：
atomic illusion

Controller workflow

目标：
eventually converge
```

这是两个非常不同的设计哲学。

---

## Terraform 也是一样

Terraform：

```
create VPC
create Subnet
create EKS
create IAM
```

并不存在一个：

```
BEGIN

AWS VPC
AWS IAM
AWS EKS

COMMIT
```

失败：

```
VPC created
Subnet created
EKS failed
```

Terraform 依赖：

```
state
retry
refresh
dependency DAG
idempotent-ish APIs
reconciliation
manual/automatic cleanup
```

而不是：

```
2PC over AWS APIs
```

为什么？

因为让所有 cloud services：

```
PREPARE
hold locks
wait for global COMMIT
```

现实上代价巨大，而且 provider 并不提供这种协议。

这也解释：

> 为什么 distributed transaction 是一个非常强、非常昂贵的 abstraction。

---

# Part 23：与 Kafka 联系

Kafka 自己支持 transaction semantics，但需要区分场景。

如果只是：

```
DB write
+
Kafka publish
```

naive：

```
write DB
publish Kafka
```

仍然有：

```
DB committed
process crashes
Kafka not published
```

于是工程上经常出现：

```
Transactional Outbox
```

```
DB transaction:
    update business table
    insert outbox event

COMMIT

CDC / relay:
    outbox -> Kafka
```

它放弃：

```
DB + Kafka instantaneous atomic visibility
```

换成：

```
durable intent
+
eventual delivery
```

这正是：

```
strong distributed transactions
vs
eventually consistent workflow
```

的工程 trade-off。

---

# Part 24：与 Redis Cache 联系

同样：

```
DB transaction
+
Redis cache
```

通常没有一个跨：

```
MySQL
Redis
```

的传统 2PC。

所以 Cache Aside：

```
write DB
invalidate Redis
```

存在 window：

```
DB new
Cache old
```

于是要依赖：

```
TTL
retry
CDC
version/fencing
double delete
eventual convergence
```

也就是说：

> 很多我们平时处理的“缓存一致性问题”，本质上就是因为我们选择了不使用一个昂贵的 cross-system atomic transaction。

---

# Part 25：Sharding 为什么导致 Distributed Transaction

如果数据库只有：

```
one shard
```

transaction：

```
UPDATE A
UPDATE B
```

可以全部交给一个 transaction manager。

Sharding 后：

```
hash(A) → Shard 1
hash(B) → Shard 7
```

于是：

```
transaction scope
>
single failure/consensus group
```

Distributed Transaction 就出现了。

所以一个常见数据库设计原则：

> **尽量让高频 transaction 的数据 colocate。**

例如电商：

```
customer_id
```

作为 shard key，使很多 customer-scoped transaction：

```
single shard
```

避免：

```
2PC
```

这不是因为 2PC 不正确，而是因为：

```
cross-shard coordination expensive
```

---

# Part 26：与 Spanner 的关系

后面 Spanner 你可以用这个 mental model 去看：

```
               transaction
                    |
          +---------+---------+
          |                   |
          v                   v
     Paxos Group 1        Paxos Group 2
```

每个 group：

```
Paxos
→ replication
```

cross-group transaction：

```
2PC
→ distributed atomicity
```

locks/concurrency protocol：

```
→ isolation
```

timestamp / TrueTime：

```
→ globally meaningful commit timestamps
→ external consistency
```

所以 Spanner 不是：

> “TrueTime 神奇地解决 distributed transactions。”

更准确：

```
Replication
+ 2PC
+ locking/concurrency control
+ MVCC
+ commit timestamps
+ TrueTime
```

共同组成最终 semantics。

---

# Part 27：为什么下一课 OCC 很自然

你已经看到 2PL 最大的问题：

```
lock
 |
transaction waits
 |
network RPC
 |
PREPARE
 |
durable log
 |
COMMIT
 |
unlock
```

如果 transaction 很长：

```
lock duration ↑
```

于是：

```
contention ↑
throughput ↓
deadlock ↑
tail latency ↑
```

于是产生一个自然问题：

> 能不能大多数时间不 lock，让 transaction 自己跑，最后再判断有没有冲突？

这就是：

```
Optimistic Concurrency Control
```

思想：

```
Read / Execute
       ↓
Validate
       ↓
if conflict:
   abort
else:
   commit
```

所以你后面 Lecture 14 OCC 的起点其实就在今天：

```
Pessimistic:

“我怕你冲突，
所以先 lock。”

Optimistic:

“我猜你不会冲突，
先做完再 check。”
```

---

# Part 28：Paper 视角

这节 Distributed Transactions 更像：

```
foundational mechanism lecture
```

而不是围绕一个单一 modern system paper。

MIT 当前材料要求阅读 6.033 Chapter 9 的相关部分，并明确指出 2PL 与 distributed 2PC 是最重要部分。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/questions.html?lec=6&q=q-raft2&utm_source=chatgpt.com)

因此这里真正应该带走的不是某篇论文里的某个 implementation trick，而是两个经典 abstraction：

```
Concurrency Control
       +
Atomic Commit
```

这些思想今天仍然存在于：

```
Spanner
CockroachDB
distributed SQL
sharded OLTP systems
```

实现细节已经非常复杂，但核心问题没有消失。

---

# Part 29：一个很有用的公式视角

2PC commit 条件不是 quorum：

假设 transaction 有：

```
N participants
```

commit 条件是：

```
YES_count = N
```

而不是：

```
YES_count > N/2
```

例如：

```
N = 3
```

三个 shard：

```
A YES
B YES
C unreachable
```

不能说：

```
2/3 majority
所以 commit
```

因为 C 可能根本无法执行 transaction。

Atomic transaction 要求：

```
A
B
C
```

所有 participating resources 都能履行 transaction。

这就是它与 Raft quorum 极其重要的区别。

---

# Part 30：一次完整 Failure Reasoning

现在做一个 6.824 风格 execution。

有：

```
TC
A
B
```

过程：

```
TC -> A PREPARE
TC -> B PREPARE

A -> YES
B -> YES

TC durable log COMMIT

TC -> A COMMIT

TC crashes
```

状态：

```
A = COMMITTED
B = PREPARED
TC = crashed
```

问：

#### B 能不能 timeout 后 abort？

不能。

因为：

```
A 已经 commit。
```

---

#### A 能不能 rollback？

不能。

因为 transaction 已经有 durable COMMIT decision。

---

#### TC reboot 后做什么？

```
read stable log

find:
T = COMMIT

resend:
COMMIT -> A
COMMIT -> B
```

A 收到 duplicate COMMIT：

```
already committed
→ idempotent success
```

B：

```
PREPARED -> COMMITTED
```

最终 convergence：

```
A committed
B committed
```

这就是 protocol state + durability + retry 组合起来实现 correctness 的方式。

---

# Part 31：Problem → Solution Chain

整节课最重要的压缩：

```
跨 shard transaction
        ↓
顺序 RPC 更新
        ↓
中途 crash 导致 partial commit
        ↓
需要 global atomic decision
        ↓
2PC
        ↓
participant YES 后 crash
        ↓
需要 durable PREPARED state
        ↓
Coordinator decision 后 crash
        ↓
需要 durable decision + retry
        ↓
Coordinator unavailable
        ↓
PREPARED participant 不知道 outcome
        ↓
2PC blocking
```

与此同时另一条线：

```
多个 transaction 并发
        ↓
读到 partial intermediate state
        ↓
每次访问加锁后马上解锁
        ↓
仍可能产生 non-serializable history
        ↓
2PL
        ↓
locks 跨 transaction 生命周期保持
        ↓
Serializability
        ↓
产生 deadlock / contention
        ↓
deadlock handling / OCC / MVCC...
```

最后合流：

```
                   Distributed Transaction
                             |
              +--------------+--------------+
              |                             |
       Concurrency correctness        Atomic outcome
              |                             |
             2PL                           2PC
              |                             |
        Serializability                  Atomicity
              +--------------+--------------+
                             |
                      correct transaction
```

---

# Part 32：三个自测题

建议你先在脑中回答，再看下面答案。

#### Level 1

```
A 和 B 都已经 PREPARED。

Coordinator crash。

A timeout。
```

A 能否 abort？

**不能。**

因为 global COMMIT 可能已经 durable，只是 A 不知道。

---

#### Level 3

```
Coordinator:

A YES
B YES
C 没回复
```

可以 COMMIT 吗？

**不能。**

2PC commit 需要所有 participant YES，不是 majority。

如果还没有 COMMIT decision，Coordinator 可以选择 ABORT。

---

#### Level 4

假设我们保留 2PC，但删掉 2PL：

```
所有 transaction 都保证 atomic commit。
```

数据库是否一定 serializable？

**不一定。**

2PC 管的是：

```
transaction outcome
```

不是：

```
transaction interleaving
```

仍可能发生 lost update、inconsistent reads 等 concurrency anomaly。

---

# Part 33：30 秒版本

面试官问：

> Distributed Transactions 这节课讲什么？

你可以回答：

> Distributed transaction 的核心是一个 transaction 跨多个独立 shard 后，需要同时解决 atomicity 和 concurrency correctness。2PC 负责 atomic commit：所有 participant 先进入 durable PREPARED 状态并投票，Coordinator 根据所有 vote 做 durable COMMIT/ABORT decision；它保证 safety，但 Coordinator 或网络故障时 prepared participant 可能 blocking。2PL 则负责 serializability，通过限制 lock acquisition/release 顺序和持有 locks 到 transaction completion，防止并发 transaction 观察到不一致的中间状态。现实 distributed DB 通常把 Raft/Paxos 用于单个 shard 的 replication，再用 2PC 协调 cross-shard transaction。

---

# Part 34：3 分钟版本

可以这样讲：

```
Distributed Transactions 有两个不同问题。

第一是 Isolation/Serializability。

多个 transaction 并发访问多个对象时，如果 lock 用完就立即释放，
可能产生不存在于任何 serial execution 中的结果。

Two-Phase Locking 要求 transaction 有 acquiring phase 和 releasing phase，
开始释放后不能再获得新 lock。
Strict 2PL 通常把 write locks 保留到 commit/abort，
从而得到 serializable execution，并避免其它 transaction
看到 uncommitted intermediate state。

第二是 Atomic Commit。

一个 transaction 如果跨多个 shard，
不能让 A commit 而 B abort。

Two-Phase Commit 的 Phase 1 让所有 participant PREPARE。
participant 只有在自己保证未来一定能够 commit 后才能回复 YES，
因此 PREPARED 状态必须 durable，而且通常需要继续持有 lock。

如果所有 participant YES，
Coordinator durable 记录 COMMIT 后进入 Phase 2，
把 COMMIT 发给所有 participant。
任何 NO 则 ABORT。

2PC 的重要 weakness 是 blocking：
participant 一旦 PREPARED 就不能自行 abort；
如果它不知道 Coordinator 的 global decision，
就必须等待，因为 decision 可能已经是 COMMIT。

因此现实 distributed DB 往往是：

each shard = Raft/Paxos replicated group

cross-shard transaction = 2PC

concurrency = 2PL/OCC/MVCC。

Raft 和 2PC 并不是同一个问题：
Raft 解决一个 replicated state machine 的 agreement，
2PC 解决多个独立 participants 的 atomic transaction。
```

---

# Part 35：深入版本

整节课最终 mental model：

```
Problem
  |
  | cross-shard atomic + concurrent updates
  v

Model
  |
  | crash-recovery
  | unreliable/delayed network
  | stable storage
  | non-Byzantine
  v

Concurrency Control
  |
  | 2PL
  v
Serializability

Atomic Commit
  |
  | PREPARE
  | durable YES
  | durable global decision
  | COMMIT / ABORT
  v
2PC

Invariants
  |
  | COMMIT only if all YES
  | YES participant remains capable of commit
  | durable COMMIT never becomes ABORT
  | conflicting locks not prematurely released
  v

Safety
  |
  | no mixed commit/abort
  | serializable committed histories
  v

Liveness
  |
  | failures eventually recover
  | coordinator/decision becomes reachable
  v

Weaknesses
  |
  | blocking
  | lock contention
  | deadlock
  | network RTT
  | durable logging cost
  v

Modern designs
  |
  +-> replicated coordinator
  +-> Paxos/Raft shards
  +-> OCC
  +-> MVCC
  +-> timestamp ordering
  +-> Spanner
```

---

# 最后的知识网络

你现在应该把今天的知识挂成这样：

```
                         Distributed Systems
                                |
              +-----------------+------------------+
              |                                    |
         Replication                          Transactions
              |                                    |
        Consensus/Raft                 +-----------+-----------+
              |                        |                       |
     State Machine Replication     Atomicity                Isolation
              |                        |                       |
        Linearizability                2PC                     2PL
                                       |                       |
                                  PREPARED              Serializability
                                       |                       |
                                  Blocking               Deadlock
                                       |                       |
                                       +-----------+-----------+
                                                   |
                                      Distributed Database
                                                   |
                                     +-------------+------------+
                                     |                          |
                                  Spanner                      OCC
```

其中最值得你真正记住的不是协议步骤，而是这四句话：

```
2PL 决定：
“并发 transaction 怎样排列才是合法的？”

2PC 决定：
“多个 participant 最终一起 commit 还是一起 abort？”

Raft / Paxos 决定：
“一个 replicated logical participant 的副本怎样 agree？”

PREPARED 决定：
“participant 已经答应可以 commit，因此失去了自行 abort 的自由。”
```

如果这四句话非常清楚，那么后面再学 **Spanner、OCC、CockroachDB distributed transactions、Percolator**，很多机制都会自然挂到同一张图上。