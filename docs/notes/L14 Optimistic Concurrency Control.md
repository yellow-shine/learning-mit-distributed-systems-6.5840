## MIT 6.824 Lecture 14 — Optimistic Concurrency Control

MIT 6.824/6.5840 的这节 **Optimistic Concurrency Control** 以 **FaRM** 为主要案例。FaRM 不是单纯讲一个数据库算法，而是在回答一个更大的工程问题：**强事务、Replication、Failure Recovery 和极低延迟能不能同时存在？** FaRM 的答案是：在低到中等 contention、内存数据库和高速 RDMA 网络下，可以通过 OCC 把绝大多数 synchronization 延迟到 commit 阶段。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

---

# Part 1：如果只能记住一个问题

> **能不能让 transaction 执行期间几乎完全不互相等待，等到 commit 时才判断：“你刚才看到的数据是否仍然有效？”**

这就是 OCC 最核心的问题。

假设：

```
x = 100
```

两个 transaction：

```
T1                         T2

r = Read(x)                r = Read(x)
x = r - 10                 x = r + 20
Commit                     Commit
```

传统 pessimistic locking 的思路是：

```
T1:

Lock(x)
Read(x)
...
Write(x)
Commit
Unlock(x)
```

如果 T1 中间因为 RPC、计算或者网络停了 500ms：

```
time →

T1: Lock(x) ---- Read(x) ---------------- Write --- Commit --- Unlock
                  |<------ 500ms ------->|

T2:             Lock(x)
                   |
                   +--------------------------- waiting
```

T2 什么也做不了。

OCC 产生了一个不同的想法：

```
T1: Read x=100 ---------------- calculate ------------------+
                                                            |
T2:      Read x=100 ------ calculate --------+              |
                                              |              |
                                          validate        validate
                                              |              |
                                            commit          abort
```

**执行阶段不阻塞。**

真正 commit 时才问：

> 我之前读取的 `x=100` 现在还是不是我认为的那个版本？

如果不是：

```
abort + retry
```

---

### 一个必须先纠正的地方

“单机没有这个问题”其实不准确。

单机数据库同样存在：

```
Transaction A
Transaction B
```

并发读写造成：

- Lost Update
- Write Skew
- Non-repeatable Read
- inconsistent state

区别是：**单机同步通常便宜得多。**

例如共享内存：

```
mutex.lock()
```

可能只是 CPU atomic instruction + cache coherence。

分布式系统里的“锁”可能意味着：

```
Client
  |
  | network
  v
Lock Server
  |
  | replication
  v
Raft / Paxos
```

再加上：

```
packet delay
server crash
network partition
lock-holder crash
recovery
replication
```

于是：

> Distributed OCC 真正想减少的不是“数据库理论上的锁”，而是昂贵的 distributed coordination。

---

# Part 2：放进整个 6.824 知识地图

先看这条主线：

```
RPC / Threads
      ↓
如何执行并发操作
      ↓
Replication / Raft
      ↓
机器挂了之后状态不能乱
      ↓
Distributed Transactions / 2PC
      ↓
一个 transaction 跨多台机器时：
all commit or all abort
      ↓
Spanner
      ↓
Distributed transaction
+ replication
+ strict serializability
      ↓
────────────────────────────────
OCC / FaRM     ← 这节课
────────────────────────────────
      ↓
如果 transaction 很多，
怎样减少 transaction 之间的 synchronization？
```

要特别把下面几个问题拆开。

---

### Raft vs OCC

Raft：

> replicas 对 command 的顺序达成一致。

```
Leader
   |
   +--> F1
   +--> F2
```

解决：

```
机器 crash 之后
大家还能不能维护同一个 state machine？
```

OCC：

> 两个 transaction 同时操作数据时，能否都合法 commit？

解决：

```
T1 reads x
T2 writes x

T1 还能不能 commit？
```

所以：

```
Raft
= replica coordination

OCC
= transaction concurrency control
```

你完全可以有：

```
OCC
   ↓
每个 shard
   ↓
Raft replication
```

---

## 2PC vs OCC

这是最重要的区别之一。

### OCC 问

```
T1 和 T2 是否 conflict？
T1 是否仍然可以 commit？
```

### 2PC 问

假设 T1 已经被允许 commit：

```
Shard A
Shard B
Shard C
```

如何保证：

```
A commit
B commit
C commit
```

而不是：

```
A commit
B abort
C commit
```

所以：

```
OCC
    ↓
这个 transaction 合法吗？

2PC
    ↓
既然决定 commit，
所有 participant 能不能一起完成？
```

两者是 **orthogonal**。

真实系统经常组合：

```
Concurrency Control
      +
Atomic Commit
      +
Replication
```

FaRM 的特别之处就在于：

> 它把这三件事高度融合，而不是简单把标准 2PC 套在 Replication 上。

---

## Spanner vs FaRM

Spanner 主要使用：

```
Paxos replication
      +
2PC
      +
locking
      +
TrueTime
```

来提供 distributed transaction 和 external consistency。

FaRM 选择：

```
Primary/Backup replication
        +
OCC
        +
RDMA
        +
NVRAM
```

FaRM 的目标环境是高性能 datacenter memory system；论文明确以 **strict serializability + high availability + 极高性能** 为目标。[Microsoft](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/SOSP15-paper227-alternate-final-version.pdf?utm_source=chatgpt.com)

可以把它们理解成：

```
Spanner:
   尽量提前防止冲突

FaRM:
   大部分执行先跑
   commit 时检查冲突
```

当然真实 Spanner 和 FaRM 都远比这张图复杂。

---

## Chain Replication vs OCC

Chain Replication：

```
Head → Replica → Replica → Tail
```

主要解决：

> 一个 replicated object 如何读写并保持一致。

OCC：

> 多个 transaction 访问多个 object 时如何保持 serializability。

所以：

```
Replication protocol
        ≠
Transaction concurrency protocol
```

---

## MapReduce / Spark vs OCC

MapReduce / Spark 更关注：

```
large-scale computation
task scheduling
data partitioning
failure recomputation
lineage
```

OCC 关注：

```
OLTP
short transactions
shared mutable state
concurrent reads/writes
```

它们处理的是完全不同类型的 distributed workload。

---

# Part 3：六个核心 Mental Models

---

## Concept 1：Optimism

#### 它解决的问题

Locking 会让没有真正 conflict 的 transaction 也承担 coordination cost。

#### 一句话

> **先假定没有 conflict，执行完以后再证明这个假定成立。**

Pessimistic：

```
可能 conflict
   ↓
先阻止你
```

Optimistic：

```
可能 conflict
   ↓
先让你运行
   ↓
commit 时检查
```

---

### 例子

1000 个用户：

```
T1 -> user/1
T2 -> user/2
T3 -> user/3
...
```

几乎完全不 conflict。

Pessimistic locking 仍然存在：

```
lock acquisition
lock metadata
lock release
distributed coordination
```

OCC 可以让：

```
Read
Compute
Buffer Write
```

大部分路径没有锁。

---

### 容易误解

> ❌ OCC = 完全没有 lock

不对。

FaRM 在 commit 阶段会锁住 **write set**。

它只是：

> execution phase 不提前持有锁。

---

## Concept 2：Read Set / Write Set

任何 OCC transaction 都可以抽象成：

```
R(T) = transaction 读过的 objects
W(T) = transaction 要修改的 objects
```

例如：

```
T:

a = Read(A)
b = Read(B)

if a > b:
    Write(C, 10)
```

那么：

```
R(T) = {A, B}
W(T) = {C}
```

为什么需要记录它？

因为 commit 时要回答：

> 从我读取它以后，有没有别人改变过这些数据？

---

## Concept 3：Version

FaRM 中 object 带一个 version。

例如：

```
Object X

value   = 100
version = 42
```

Transaction 读：

```
Read(X)
→ value=100
→ version=42
```

transaction 保存：

```
readSet = {
    X: version 42
}
```

commit 时：

```
X.version == 42 ?
```

如果：

```
X.version = 43
```

说明：

> 在 transaction 执行期间，有别的 transaction 修改了 X。

FaRM 的每个 object 使用 64-bit version 做 concurrency control 和 replication。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Concept 4：Validation

这就是 OCC 的灵魂。

可以把 commit 前的检查简化成：

\[ \forall o \in R(T): version_{now}(o)=version_{read}(o) \]

先解释符号。

`∀`：

> 对所有。

`o`：

> 一个 object。

`R(T)`：

> transaction T 的 Read Set。

所以：

> 对 T 所有读取过的 object，现在的 version 必须仍然等于它当时读取的 version。

例如：

```
Read:

A v7
B v12
C v5
```

commit：

```
A v7   ✓
B v13  ✗
C v5   ✓
```

那么：

```
Abort
```

即使 A/C 没问题也不行。

---

## Concept 5：Serialization Point

Serializability 最终需要找到：

> 整个 transaction 仿佛在某一个瞬间原子执行。

FaRM 对 read-write transaction 选择：

> **成功 acquire 所有 write locks 的那个时刻**

作为 serialization point。论文证明 committed read-write transactions 可以在这里被序列化；read-only transaction 则可在最后一次读取的位置序列化。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

先把它画出来：

```
                  serialization point
                          ↓
time ------------------------------------------------>

Read A
Read B
Compute
       Lock X
       Lock Y
             Validate A/B
                        Replicate
                               Install Writes
                                      Return success
```

有一个很反直觉的问题：

> validation 明明发生在 serialization point 后面，怎么能这样？

这个非常重要，我们后面证明。

---

## Concept 6：Strict Serializability ≠ OCC

OCC 是：

```
implementation technique
```

Strict Serializability 是：

```
consistency guarantee
```

不能说：

```
用了 OCC
=> 自动 strict serializable
```

完全不成立。

你可以设计一个错误的 OCC。

FaRM 的 commit protocol 才最终保证：

```
successful committed transactions
→ strict serializability
```

论文明确给 committed transactions 提供 strict serializability。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

# Part 4：一个极重要的区别

### Serializability vs Strict Serializability

Serializability：

> concurrent execution 看起来等价于某个 serial order。

例如现实：

```
T1 和 T2 overlap
```

只要结果等价于：

```
T1 → T2
```

或者：

```
T2 → T1
```

都行。

---

Strict Serializability 再增加：

> 必须遵守 real-time ordering。

如果：

```
time →

T1: ----- commit ---->
                     T2: ----- start ---->
```

即：

```
T1 已经完成
然后 T2 才开始
```

serial order 不能是：

```
T2 → T1
```

必须：

```
T1 → T2
```

可以用：

\[ SP(T) \in [start(T), response(T)] \]

理解。

`SP(T)`：

```
serialization point
```

只要每个 transaction 的 serialization point 都位于：

```
transaction 开始
     和
client 收到结果
```

之间，real-time ordering 就能自然成立。

---

## Linearizability vs Strict Serializability

你已经学过 Linearizability，可以这样联系：

```
Linearizability
→ 通常描述单个 operation / object

Strict Serializability
→ transaction-level analogue
```

例如：

```
Transaction:
Read A
Write B
Write C
```

要求整个 transaction 像一次 atomic operation。

---

## Snapshot Isolation vs OCC

这两个也不要混。

Snapshot Isolation：

> transaction 读取某个 consistent snapshot。

OCC：

> concurrency control implementation strategy。

完全可以：

```
MVCC + OCC
```

也可以：

```
single-version + OCC
```

FaRM 这篇论文实际上只维护单一 object version。

---

# Part 5：先构造一个最简单的 OCC

假设：

```
A = 100, version 5
B = 20,  version 8
```

Transaction T：

```
a = Read(A)
b = Read(B)

Write(A, a - 10)
Write(B, b + 10)
```

---

### Phase 1：Execute

读取：

```
A:
value=100
version=5

B:
value=20
version=8
```

但 **不直接改数据库**。

本地：

```
readSet:

A -> v5
B -> v8
```

以及：

```
writeBuffer:

A -> 90
B -> 30
```

数据库此时仍然：

```
A = 100
B = 20
```

这叫：

> buffered writes。

FaRM execution phase 通过 local access 或 one-sided RDMA 读取对象，同时将 writes 保存在 coordinator 本地，并记录 object address/version。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## 为什么不能直接写？

假设：

```
T writes A=90
```

然后发现：

```
B changed
```

transaction 要 abort。

那你必须：

```
rollback A
```

非常麻烦。

所以：

```
speculative execution
        ↓
writes stay private
```

---

# Part 6：第一次 Failure

现在两个 transaction：

```
T1                          T2

Read X(v10)                 Read X(v10)

X = 100                     X = 200
```

如果 commit 时只是：

```
if version == 10:
    write
```

可能这样：

```
time →

T1: check X=v10 ✓
T2:        check X=v10 ✓
T1:              write X=100
T2:                    write X=200
```

两个 validation 都成功。

这是经典：

> check-then-act race。

---

## 新机制：Lock Write Set

FaRM commit 的第一阶段：

```
LOCK
```

Coordinator 请求 primary：

```
Lock X only if:

version == expectedVersion
AND
not already locked
```

可以理解成：

```
CAS(
    state = (version=10, unlocked),
    state = (version=10, locked)
)
```

T1：

```
CAS success
```

T2：

```
CAS fail
→ abort
```

FaRM 的 primary 正是使用 compare-and-swap，根据 expected version 锁定对象；如果版本已经变化或者 object 被另一 transaction 锁定，则 transaction abort。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## 注意

所以：

> **OCC 并不是 no-lock concurrency control。**

FaRM 是：

```
Execution:
    optimistic
    no write locks

Commit:
    pessimistically freeze write set
```

这是非常经典的 hybrid 思路。

---

# Part 7：第二次 Failure

现在：

```
T1:

Read(A)
Write(B)
```

T2：

```
Write(A)
```

假设：

```
T1 reads A=10
```

随后：

```
T2 changes A=20
```

T1 的 write set 只有：

```
B
```

因此：

```
Lock(B) succeeds
```

如果到这里直接 commit：

```
T1 用一个已经过期的 A
计算出了 B
```

错误。

---

## 新机制：Read Validation

所以 acquire write locks 之后：

```
for every object:
    Read Set - Write Set
```

重新看 version。

例如：

```
T1 originally:

A = v7
```

现在：

```
A = v8
```

那么：

```
Abort
```

FaRM 在 lock phase 后重新读取所有“read but not written”的 object version；任意一个发生变化就 abort。默认使用 one-sided RDMA validation。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

# Part 8：完整 Happy Path

现在看真正 FaRM transaction。

假设 transaction：

```
Read A
Read B
Write X
Write Y
```

数据分布：

```
P1: X
P2: Y
P3: A, B

B1: X backup
B2: Y backup
```

Coordinator：

```
C
```

---

### Step 0 — Execute

```
C
|
+---- RDMA Read A(v4)
|
+---- RDMA Read B(v9)
|
+---- Read X(v7)
|
+---- Read Y(v2)
```

记录：

```
Read Set:
A v4
B v9
X v7
Y v2
```

计算：

```
new X
new Y
```

本地 buffer：

```
Write Set:
X -> newX
Y -> newY
```

---

### Step 1 — LOCK

```
                LOCK X(v7)
C --------------------------------> P1

                LOCK Y(v2)
C --------------------------------> P2
```

Primary：

```
CAS(version + lock bit)
```

全部成功。

现在：

```
X locked
Y locked
```

#### Serialization Point

此时：

```
               ★
               ↓
------- all write locks acquired -------
```

---

## Step 2 — VALIDATE

重新检查：

```
A == v4 ?
B == v9 ?
```

假设：

```
yes
yes
```

所以执行期间看到的 read set 是合法的。

---

## Step 3 — COMMIT BACKUPS

现在不能立刻修改 primary。

先：

```
C ---------- COMMIT-BACKUP ----------> B1
C ---------- COMMIT-BACKUP ----------> B2
```

写入 durable NVRAM log。

等待 NIC hardware acknowledgement。

FaRM 的第三阶段是在 backups 的 non-volatile transaction logs 中写 `COMMIT-BACKUP`，然后等待这些 writes 的 hardware acknowledgement。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Step 4 — COMMIT PRIMARIES

backup 已 durable 后：

```
C -------- COMMIT-PRIMARY --------> P1
C -------- COMMIT-PRIMARY --------> P2
```

Primary：

```
install new value
increment version
unlock
```

比如：

```
X:
old value
v7
locked

↓

new value
v8
unlocked
```

此时变化对其他 transactions 可见。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Step 5 — TRUNCATE

等 transaction 已经安全完成后：

```
transaction logs
```

不需要永远保存。

FaRM 会 lazy truncate log，并让 backup 最终把 updates apply 到自己的 object copies。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## 整体时间线

```
time ---------------------------------------------------------->

Coordinator:
    Execute
       |
       | LOCK
       +---------------> P1
       +---------------> P2
                         |
                 ★ Serialization Point
                         |
                         | Validate
                         +---------- RDMA reads --------> P3
                         |
                         | Commit Backup
                         +-----------------------------> B1
                         +-----------------------------> B2
                         |
                         | Commit Primary
                         +-----------------------------> P1
                         +-----------------------------> P2
                                                       |
                                                  expose data
                                                       |
                                                return success
```

论文中的 FaRM commit protocol 正是：

```
LOCK
VALIDATE
COMMIT-BACKUP
COMMIT-PRIMARY
TRUNCATE
```

其中前四个构成关键 commit 流程。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

# Part 9：这里真正难懂的地方

### 为什么 Serialization Point 可以放在 validation 之前？

你可能会觉得：

```
Lock write set
      ↓
★ serialization point
      ↓
Validate read set
```

不合理。

因为：

> “我还没检查 read set，怎么已经决定 serialization point 了？”

答案是：

> validation 不是“创造”那个历史，而是在事后证明那个历史成立。

假设 T：

```
Read A=v5
Write X
```

时序：

```
read A=v5

          ★ lock X

                     validate A=v5
```

validation 发现：

```
A 仍然是 v5
```

那么可以推出：

```
在 ★ 时刻
A 也一定还是 v5
```

前提是 version 每次修改单调改变。

所以在 ★：

```
T 的所有 read values
```

仍然有效。

而 write objects 此时又全部被 T lock 住。

因此可以想象：

> transaction 就是在 ★ 的瞬间原子发生。

---

### 如果 A 在 ★ 之后改变呢？

例如：

```
read A=v5

        ★

another transaction:
write A -> v6

        validate sees v6
```

T 会：

```
abort
```

但其实 theoretically：

```
T
```

完全可以 serialize 在 A 变化之前。

也就是说：

> OCC 可能产生 **false/conservative abort**。

这没破坏 Safety。

只是降低性能。

这是一个很重要的原则：

```
Safety:
宁可 abort 一个本来可以 commit 的 transaction

也不能：
commit 一个不合法的 transaction
```

---

# Part 10：FaRM 一个非常反直觉的性质

假设：

```
x = 1
y = 1
```

T1 原子修改：

```
x = 2
y = 2
```

commit primary 时，因为对象可能分布不同机器：

```
P1: x = 2
P2: y = 1
```

可能短暂存在。

T2：

```
Read(x) -> 2
Read(y) -> 1
```

你可能会说：

> 这不是违反 transaction atomicity 了吗？

**执行过程中，FaRM 确实允许这种情况。**

FaRM 保证：

- 单个 object read 是 atomic 的；
- read 到的是 committed data；
- 但**不同 object 的 reads 不一定形成 consistent snapshot**。

如果读取组合不一致，T2 最终 validation 会失败，因此 **T2 不能 commit**。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

这是 Lecture 14 非常容易漏掉的一点。

---

## OCC ≠ Opacity

Opacity 比 Serializability 更强的一点是：

> 连最终会 abort 的 transaction，在执行过程中也不能看到 inconsistent state。

FaRM 2015 **没有提供这个保证**。

所以 application 可能看到：

```
x = new
y = old
```

然后必须避免因为这个临时 inconsistent state：

```
divide by zero
invalid pointer
send irreversible external request
```

等问题。

FaRM 后续研究专门加入了 **Opacity** 支持。[Microsoft](https://www.microsoft.com/en-us/research/wp-content/uploads/2019/01/mod057.pdf?utm_source=chatgpt.com)

---

# Part 11：State

可以把 FaRM transaction coordinator 的核心 state 想象成：

```
TransactionContext
{
    txID

    readSet[]:
        object address
        version

    writeSet[]:
        object address
        expectedVersion
        newValue

    participants[]

    phase
}
```

其中：

#### `readSet`

谁修改：

```
transaction execution
```

作用：

```
commit validation
```

持久化？

```
No
```

FaRM 的 read set 只存在 coordinator；这对 crash recovery 有重要影响。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

#### `writeSet`

包含：

```
expected version
new value
```

LOCK 时用于：

```
version check
```

COMMIT 时用于：

```
install new value
```

---

### Object State

概念上：

```
Object {
    value
    version
    locked
}
```

关键 invariant：

```
version changes whenever committed value changes
```

否则：

```
ABA-style problem
```

会让 validation 错误认为：

```
nothing changed
```

---

## Participant Logs

Primary / backup 保存 transaction logs，例如：

```
LOCK
COMMIT-BACKUP
COMMIT-PRIMARY
ABORT
TRUNCATE
```

这些是 crash recovery 的依据。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Cluster State

FaRM 还有：

```
configuration ID
membership
region → primary/backups
failure domains
Configuration Manager
```

ZooKeeper 用于确保大家对 current configuration 达成一致，而 FaRM 自己负责快速 leases、failure detection 和 recovery。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

# Part 12：核心 Invariants

### Invariant 1

> committed transaction 的所有 write-set objects，在 serialization point 时必须都仍然处于 transaction originally observed 的版本。

否则：

```
Lost Update
```

可能出现。

---

## Invariant 2

> committed transaction 读取但未写的 objects，在 serialization point 时必须仍然与 execution phase 看到的 version 相同。

Validation 保证这一点。

---

## Invariant 3

> 一个 transaction 的 writes 在 commit 之前不能对其他 transaction 成为“committed state”。

因此：

```
execution writes
→ private buffer
```

---

## Invariant 4

> 在 primary 将 committed state 暴露之前，必须存在足够 durable 的 transaction state 支持 crash recovery。

所以：

```
COMMIT-BACKUP
      ↓
wait durable ACK
      ↓
COMMIT-PRIMARY
```

不能反过来。

---

## Invariant 5

> client 收到 successful commit 后，即使允许范围内的机器发生 failure，系统之后不能把这个 transaction 变成 aborted。

这是 distributed transaction 非常核心的：

```
external durability
```

---

# Part 13：Correctness

现在真正证明 OCC 为什么正确。

---

## Safety

我们要证明：

> 两个 committed transactions 的结果一定能解释为一个合法 serial execution。

假设 read-write transaction T 的 serialization point：

```
SP(T)
=
all write locks successfully acquired
```

---

### 对 Write Set

所有 write object：

```
X
Y
Z
```

在 SP 时：

```
locked by T
```

而且 LOCK 使用 expected version。

因此：

```
version(X) == version T saw
version(Y) == version T saw
version(Z) == version T saw
```

否则 lock 根本不会成功。

---

### 对 Read-Only portion

例如：

```
A
B
```

T 后续 validation：

```
version(A) unchanged
version(B) unchanged
```

因为：

```
validation time >= SP
```

而 validation 时仍相同，所以它们在 SP 时也没有发生早于 validation 的改变，否则 version 会不同。

所以：

```
SP 时：

A/B/X/Y/Z
```

全部都与 transaction 计算时依赖的状态兼容。

因此我们可以把：

```
T
```

想象成在：

```
SP
```

原子执行。

这就是 serializability proof 的核心。论文同样将 locking 和 validation 分别作为 write-set 与 read-only-set 正确性的依据。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Strictness

FaRM 的 serialization point：

```
after transaction starts
before client learns completion
```

因此：

```
T1 completes
↓
T2 starts
```

必然：

```
SP(T1) < SP(T2)
```

于是尊重 real-time ordering。

所以是：

```
Strict Serializability
```

而不仅仅是 Serializability。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Liveness

Safety 只说：

> 不会 commit 错东西。

Liveness 要问：

> transaction 最终能不能推进？

在 contention 很低而网络稳定时：

```
locks eventually succeed
validation succeeds
replication succeeds
transaction commits
```

但 OCC 的 liveness 有一个明显问题：

```
hot key
```

例如：

```
1000 transactions
       ↓
all update same X
```

可能：

```
999 abort
1 commit
```

然后 999 个 retry：

```
again conflict
again abort
```

所以：

> OCC Safety 很漂亮，但 Liveness / throughput 高度依赖 workload contention。

---

# Part 14：System Model

这里我们区分：

```
generic OCC
```

和：

```
FaRM paper model
```

下面说 FaRM。

---

### Node Model

FaRM 假设 machine 可以：

```
crash
```

但不是：

```
Byzantine
```

也就是说不会：

```
maliciously fabricate versions
forge log records
send contradictory protocol messages
```

论文假设 crashed machine 可以恢复，并且 **non-volatile DRAM 内容不会因普通 crash 丢失**。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

所以更接近：

```
crash-recovery
```

而不是纯 crash-stop。

---

## Storage Model

数据：

```
DRAM
```

但通过当时论文提出的 battery/UPS + SSD backup 机制把 DRAM 当作：

```
non-volatile DRAM
```

FaRM 将 transaction commit 所需状态复制到多个 NVRAM replicas 才认为 durable。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

注意：

> 这是 2015 年论文的具体 hardware design，不是 OCC 本身的必要条件。

今天实现 OCC 完全可以基于：

```
SSD
PMem
WAL
replicated log
```

---

## Replication Model

每个 region：

```
1 primary
+
f backups
```

即：

\[ N = f + 1 \]

例如：

```
f = 2
```

那么：

```
Primary
Backup1
Backup2
```

共：

```
3 copies
```

论文用 primary-backup 而不是每个 participant 都跑 Paxos，因此相较 `2f+1` SMR replicas，FaRM 在自己的 failure/configuration model 中只存 `f+1` copies。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

这不能简单理解成：

> “Consensus 原来只需要 f+1。”

不是一回事。

---

## Timing Model

论文明确：

```
bounded clock drift
```

用于 Safety，并依赖：

```
eventually bounded message delay
```

获得 Liveness。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

因此 mental model 上可以视为：

> **partially synchronous system**

而不是纯 asynchronous。

---

## Network

网络可以：

```
delay
partition
```

FaRM 使用：

```
RDMA
```

作为主要 data path。

还通过：

```
leases
precise membership
configuration changes
```

处理机器 failure / network partitions。

---

## Network Partition

FaRM 在 partition 下还能保持 availability 的条件非常具体：

需要存在一个 partition：

```
包含多数 FaRM machines
AND
能连到 ZooKeeper majority
AND
包含每个 object 至少一个 replica
```

才可继续提供服务。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

其它 minority partition：

```
不能继续随意写
```

否则会 split brain。

---

## Failure Tolerance

对象：

```
Primary + f backups
```

系统保证：

> 在每个 object 最多 `f` 个 replicas 丢失 NVRAM 内容时，committed state 仍可 durable。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

如果超过：

```
f failures
```

可能：

```
data unavailable
```

甚至：

```
durability guarantee lost
```

取决于具体丢失情况。

---

# Part 15：Failure Scenario

现在逐个加 failure。

---

## Failure 1：Write conflict

```
T1: Read X(v10)
T2: Read X(v10)

T1: Lock X(v10) ✓
T2: Lock X(v10) ✗
```

结果：

```
T1 continues
T2 abort
```

Safety：

```
safe
```

Availability：

```
yes
```

只是 T2 重试。

---

## Failure 2：Read Set 被别人修改

```
T1:
Read A(v5)
Write B

T2:
Write A -> v6
Commit

T1:
Lock B ✓
Validate A
```

看到：

```
v6 != v5
```

因此：

```
T1 abort
```

这是：

> optimistic conflict detection。

---

## Failure 3：Coordinator crash before commit

假设：

```
LOCK 完成
```

然后 coordinator crash。

此时可能：

```
write objects locked
```

不能永远卡住。

FaRM recovery 会利用 participant transaction logs 恢复 in-flight transaction 状态。

非常关键的是：

> coordinator 本身没有复制完整 read set。

如果没有足够 commit evidence：

```
transaction can be aborted
```

这也是为什么 failure recovery protocol 要区分：

```
LOCK
COMMIT-BACKUP
COMMIT-PRIMARY
```

这些不同阶段。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Failure 4：Primary exposes write，然后崩溃

假设错误协议：

```
Commit Primary
     ↓
Expose X=10
     ↓
then replicate Backup
```

时间线：

```
P1: expose X=10
         |
         X crash

Backup:
X still old
```

现在 failover：

```
new primary
→ old value
```

之前 committed 数据消失。

所以 FaRM 顺序必须：

```
Commit Backups
      ↓
wait ACKs
      ↓
Commit Primary
```

论文特别指出，如果没等所有 required backup acknowledgements 就 expose primary changes，之后 failures 可能导致 committed update 丢失。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## Failure 5：Primary commit 完成，但 coordinator crash before client reply

这是经典 distributed systems ambiguity：

```
Database:
COMMITTED

Coordinator:
crash

Client:
timeout
```

client 无法根据 timeout 判断：

```
transaction committed?
```

还是：

```
transaction aborted?
```

这和：

```
RPC exactly-once impossibility
```

是同一类问题。

OCC 本身不解决 application-level retry semantics。

通常需要：

```
Transaction ID
request ID
deduplication
query transaction status
idempotency
```

---

## Failure 6：Packet Delay

假设：

```
COMMIT-BACKUP
```

迟迟没有 ACK。

正确系统不能说：

```
等太久了
所以假装已经 durable
```

timeout 只能意味着：

```
I don't know
```

不是：

```
operation definitely failed
```

这又连接回 Lecture 2 RPC。

---

# Part 16：Failure Matrix

|Failure|Transaction 怎么办|Safety|Availability|
|---|---|---|---|
|write-write conflict|一方 lock 失败并 abort|保持|有|
|read object changed|validation abort|保持|有|
|coordinator execution phase crash|speculative state 丢掉|保持|有|
|coordinator commit 中 crash|recovery 根据 participant logs 决定|保持|暂时受影响|
|primary crash|reconfiguration / promote replica|保持于 failure assumption 内|短暂下降|
|backup crash|只要 fault budget 允许，可恢复|保持|通常有|
|delayed message|等待 / timeout / recovery|保持|可能暂停|
|network partition|合法 configuration 一侧继续|保持|minority 不可用|
|node restart|从 NVRAM/recovery state 恢复|保持|恢复后继续|
|> f replicas 丢失|guarantee 不再成立|可能失去 durability|可能不可用|
|hot-key contention|大量 abort/retry|保持|吞吐可能崩|

---

# Part 17：为什么 OCC 在 low contention 特别好

假设：

```
100 transactions
```

只有：

```
1 transaction
```

最终发生冲突。

Pessimistic model：

```
100 次都付 synchronization cost
```

OCC：

```
99 个直接成功
1 个 abort/retry
```

粗略 mental model：

\[ Cost_{OCC} ≈ Execution + Validation + AbortRate \times WastedWork \]

当：

```
AbortRate 很低
```

就很划算。

---

## 高 contention 呢？

假设：

```
hot counter X
```

1000 transactions：

```
Read X
X++
Commit
```

大家同时：

```
Read X = 100
```

最终：

```
1 commit
999 abort
```

这些 transaction 的：

```
reads
network
CPU
business logic
validation
```

全浪费了。

此时 pessimistic：

```
queue behind a lock
```

反而可能更好。

所以：

```
Low contention
    ↓
OCC attractive

High contention
    ↓
Locking / queueing / batching
可能更好
```

---

## Long Transaction 对 OCC 很不友好

假设一个 transaction：

```
Read A
Read B
...
compute for 3 seconds
...
Read Z
Commit
```

它活得越久：

```
someone changing its read set
```

的概率越高。

所以：

```
long transaction
+ large read set
+ high contention
```

是 OCC 的典型坏场景。

---

# Part 18：为什么没有 Deadlock？

传统 2PL：

```
T1 holds X
waits Y

T2 holds Y
waits X
```

形成：

```
T1 -> T2
^       |
|       v
+-------+
```

deadlock。

FaRM lock phase：

```
如果 object 已经 locked
→ abort
```

而不是：

```
wait forever
```

因此这种设计避免了 traditional lock-wait deadlock。

代价：

```
more aborts
```

又是一个经典：

```
waiting
vs
retry
```

trade-off。

---

# Part 19：OCC 和 MVCC

很多现代数据库会组合：

```
MVCC
+
OCC
```

例如 transaction 读取：

```
snapshot version
```

然后 commit 时：

```
validate conflicts
```

MVCC 主要帮助：

```
readers do not block writers
historical snapshot reads
```

OCC 主要帮助：

```
决定 transaction 是否仍能 commit
```

FaRM 2015 的设计不同：

> object 本身保持单个 persistent version，并用 version + validation 实现其 OCC protocol。

---

# Part 20：Top 5 Misconceptions

### ❌ 1. OCC 完全不用锁

错误。

FaRM：

```
Execution
→ lock-free-ish

Commit
→ lock write set
```

“Optimistic”指的是：

> 不在整个执行期间占锁。

---

### ❌ 2. OCC transaction 读到的永远是 consistent snapshot

错误。

FaRM 可以出现：

```
Read X = new
Read Y = old
```

只保证：

> 如果这个组合不能形成合法 committed transaction，它最终 commit validation 会失败。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

### ❌ 3. OCC 自动保证 Serializability

错误。

错误设计：

```
Read
check versions
then later write
```

check 和 write 之间还有 race。

必须设计：

```
locking
version checks
serialization point
atomic commit
```

才能证明。

---

### ❌ 4. OCC 和 2PC 是两种互斥方案

错误。

分别回答：

```
OCC:
Should this transaction commit?

2PC:
How do all participants agree on commit/abort?
```

一个系统完全可以：

```
OCC + 2PC
```

---

### ❌ 5. Abort 是异常

在 OCC 中：

```
abort
```

是正常控制流。

就类似 CAS：

```
compare_exchange()
```

失败不是系统坏了，只表示：

> somebody won the race before you.

---

# Part 21：FaRM 为什么特别适合 RDMA

传统 request：

```
Client
  |
  | request
  v
Server CPU
  |
  | lookup
  v
Memory
  |
  | reply
  v
Client
```

One-sided RDMA：

```
Client NIC
     |
     +-----------------> Remote NIC
                             |
                             v
                          Memory
```

remote CPU 不必进入 fast path。

所以：

```
Read object
Read version
Validation
Write log record
```

大量操作都可以利用 RDMA。

FaRM 论文的关键系统设计之一就是：

> 使用 one-sided RDMA 减少 remote CPU involvement。[Microsoft](https://www.microsoft.com/en-us/research/project/farm/?utm_source=chatgpt.com)

---

## 这和 OCC 天然匹配

如果 execution 期间每次 read 都要：

```
distributed lock RPC
```

那 RDMA 的价值会被 coordination 吃掉。

OCC：

```
Execution:
RDMA read
RDMA read
RDMA read

Commit:
一次集中 coordination
```

所以：

> OCC 不仅是数据库算法选择，也是一个 hardware-aware design choice。

---

# Part 22：FaRM 与 Replication 的结合

普通思维可能是：

```
Transaction Layer
      ↓
2PC
      ↓
Replication Layer
      ↓
Paxos/Raft
```

层层叠：

```
Coordinator
    ↓
Participant
    ↓
Participant's Paxos
    ↓
replica quorum
```

message 非常多。

FaRM：

```
Coordinator
   |       \
   |        \
Primary    Backup
```

直接写 primary / backup logs。

论文强调：

> transaction protocol 与 replication protocol 是共同设计的，而不是完全分层。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

这是一个很重要的 systems lesson：

> Abstraction 很重要，但极端性能系统经常通过 cross-layer design 减少 round trip 和 CPU overhead。

---

# Part 23：Failure Recovery

FaRM recovery 大致分成：

```
Failure Detection
      ↓
Reconfiguration
      ↓
Transaction State Recovery
      ↓
Bulk Data Recovery
      ↓
Allocator Recovery
```

论文使用非常短的 leases 做 failure detection，并通过 configuration manager 和 ZooKeeper 建立新的 precise membership。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

## 为什么 membership 这么重要？

RDMA 有一个特殊困难。

普通 RPC server：

```
request arrives
      ↓
server CPU
      ↓
check:
"我现在还是合法 primary 吗？"
```

RDMA：

```
remote NIC
直接读 memory
```

remote CPU 甚至看不到 operation。

于是不能依赖：

```
server checks lease for every request
```

FaRM 的解决思想是：

> 让整个 cluster 先对新的 membership/configuration 达成一致，然后 client 只对 current members 发 RDMA；旧 configuration 的 responses 被忽略。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

这个设计非常值得记。

---

# Part 24：和 Kubernetes / Infrastructure 联系

不要强行把 Kubernetes controller 当成 OCC，但有几个很有用的联系。

---

### Kubernetes ResourceVersion

Kubernetes API 中经常出现：

```
resourceVersion
```

mental model 很像：

```
Read object
remember version

Update:
"only if it is still this version"
```

也就是：

```
optimistic compare-and-swap
```

例如两个 controller 同时：

```
GET Deployment v100
```

然后都想 update。

第一个：

```
v100 -> v101
```

第二个拿旧 `v100` 更新：

```
Conflict
```

然后：

```
re-read
reconcile
retry
```

这和 OCC 的核心思想高度相似：

```
observe version
      ↓
do work
      ↓
validate expected version
      ↓
retry on conflict
```

但 Kubernetes 一个 API update 的 optimistic locking：

```
不是自动等价于 multi-object serializable transaction
```

区别非常重要。

---

## Terraform 也有类似 Mental Model

Terraform：

```
Read remote state
       ↓
Plan
       ↓
Apply
```

中间现实世界可能发生变化。

如果想象一个理想 OCC Terraform：

```
Observe:
VPC version = 5

Plan based on v5

Commit:
is VPC still version 5?
```

如果不是：

```
abort / re-plan
```

Terraform 实际机制不等价于数据库 OCC transaction，但这种：

```
observe
compute
validate assumption
retry
```

思维非常类似。

---

## Kubernetes Controller

Controller：

```
Observe
   ↓
Compute desired action
   ↓
Act
   ↓
Conflict?
   ↓
Re-read
   ↓
Reconcile again
```

某种意义上是一种：

> optimistic retry-oriented programming model。

但区别仍然是：

```
Controller:
eventual convergence

Database OCC:
transaction atomicity + serializability
```

不要混起来。

---

# Part 25：Kafka / Redis

### Redis

单线程 Redis command：

```
INCR x
```

天然串行。

但 client：

```
GET x
compute
SET x
```

仍可能：

```
Lost Update
```

Redis 的：

```
WATCH
MULTI
EXEC
```

有明显的 optimistic concurrency flavor：

```
WATCH key
      ↓
Read/prepare
      ↓
如果 key changed
EXEC fails
```

这与 OCC mental model 很接近。

---

## Kafka

Kafka 本身的：

```
partition ordering
```

解决：

> record ordering。

不是：

> arbitrary database transaction concurrency control。

Kafka transactions 更多处理：

```
atomic writes across partitions
read_committed visibility
producer fencing
```

不要直接等同 OCC。

---

# Part 26：与你熟悉的 Fencing Token 的关系

你之前问过 fencing token。

Fencing token：

```
old owner: token=10
new owner: token=11
```

resource 拒绝：

```
token < 11
```

它解决：

> stale owner 恢复后继续做 destructive action。

Version validation：

```
Read X at v10
```

commit：

```
only modify if X is still v10
```

二者共同 mental model 是：

> **不要相信“我曾经是合法的”；要求操作携带能够证明自己仍然合法的 monotonic version。**

但：

```
fencing
```

主要解决 stale ownership。

```
OCC version
```

主要解决 transaction conflict。

---

# Part 27：Paper Problem

FaRM paper 的出发点不是：

> “让我们发明一个新的 OCC。”

更大的问题是：

> **为什么 distributed transactions 一直被认为太慢？现代 hardware 能不能改变这个结论？**

以前很多系统选择：

```
weak consistency
```

或者：

```
single-partition transaction only
```

因为跨机器强事务代价太高。

FaRM 的 thesis 是：

> 通过 RDMA、non-volatile memory 和重新设计 transaction/replication/recovery protocol，可以同时获得 strict serializability、availability 和很高性能。[Microsoft](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/SOSP15-paper227-alternate-final-version.pdf?utm_source=chatgpt.com)

---

## Previous Approach

传统：

```
Application
     ↓
2PC coordinator
     ↓
Participant
     ↓
Paxos replicas
```

容易出现：

```
many RPCs
many CPU interrupts
many round trips
many durable writes
```

尤其内存操作只有几十纳秒、几百纳秒之后：

> 软件 protocol 的 coordination overhead 反而成为主要成本。

---

## Key Insight

FaRM 的核心系统洞察可以压缩成：

```
Fast hardware only helps
if the protocol stops forcing the CPU/network
to do unnecessary coordination.
```

具体：

```
OCC
→ execution phase no distributed locking

RDMA
→ remote reads without remote CPU

NVRAM
→ durability without slow disk fast path

Primary/Backup
→ fewer replicas/messages

Integrated commit + replication
→ avoid layered protocol overhead
```

---

# Part 28：Paper Architecture

```
                 ZooKeeper
                     |
              configuration
                     |
                     v
             Configuration Manager
                     |
     +---------------+---------------+
     |               |               |
 Machine A        Machine B        Machine C
     |               |               |
 Application       Region          Region
 Thread             Primary         Backup
     |
 Transaction
 Coordinator
     |
     +---- one-sided RDMA reads ------+
     |
     +---- transaction log writes ----+
```

FaRM 暴露一个 distributed global address space，application thread 可以在 transaction 中跨机器读写 objects。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

# Part 29：Evaluation

这些数字应当理解为：

> **2015 FaRM paper 在其专门 hardware/testbed 上的实验结果**

不是 2026 的通用数据库 benchmark。

论文报告：

```
90 machines
4.9 TB database

TATP:
~140 million transactions / second
```

并报告 single-machine failure 后恢复到高吞吐的时间在几十毫秒量级。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

TPC-C 实验中，论文报告最高约：

```
4.5 million new-order tx/s
median ~808 µs
99th ~1.9 ms
```

在该测试环境和实现下。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

论文最大的 evaluation 信息并不是某个具体数字，而是：

> distributed strict-serializable transactions 不一定天然意味着“慢几个数量级”。

---

# Part 30：Limitations

最重要的几个。

#### 1. High contention

OCC：

```
conflict
→ abort
→ wasted work
```

hotspot 很严重时性能可能快速恶化。

---

#### 2. Large transactions

read set 越大：

```
validation cost ↑
conflict probability ↑
```

---

#### 3. Long transactions

transaction 越久：

```
read version becoming stale
```

的概率越高。

---

#### 4. Temporary inconsistent reads

FaRM 2015 execution 阶段不提供跨对象 consistent snapshot，因此 application 必须能承受 temporary inconsistency。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/farm-2015.pdf)

---

#### 5. Hardware-dependent optimization

FaRM 很多优势来自：

```
RDMA
high-speed NIC
memory resident database
NVRAM assumptions
```

不能简单复制到：

```
internet-scale WAN
slow disks
high-latency region-to-region network
```

还期待同样结果。

---

# Part 31：What aged well?

这些思想今天仍非常重要：

```
optimistic concurrency
version validation
lock-free reads
buffered writes
cross-layer transaction + replication optimization
hardware-aware systems design
fast failure recovery
precise membership
```

尤其：

```
read → compute → compare version → retry
```

在数据库、KV store、control plane、API concurrency control 中到处存在。

---

## What changed?

2015 之后 hardware 和系统设计继续演进：

```
faster RDMA / SmartNIC
NVMe
persistent-memory research
cloud-native distributed DB
MVCC systems
hybrid OCC
```

FaRM 自己后续研究也加强了 transaction execution guarantees，包括 **Opacity**，避免 aborted transaction 暂时观察 inconsistent state。[Microsoft](https://www.microsoft.com/en-us/research/project/farm/publications/?utm_source=chatgpt.com)

---

# Part 32：OCC vs 2PL 一张表看懂

||Pessimistic 2PL|OCC|
|---|---|---|
|核心思想|先防止 conflict|先执行，再检查|
|execution 时 lock|通常有|通常没有|
|conflict 代价|waiting|abort/retry|
|deadlock|可能|fail-fast OCC 通常避免|
|low contention|lock overhead 可能浪费|通常很好|
|high contention|queue 可能更稳定|abort storm|
|long transaction|长时间占锁|abort probability 高|
|read-heavy|lock/metadata cost|很适合|
|correctness|lock rules|validation + commit protocol|

---

# Part 33：Problem → Solution Chain

这是这节课最值得记住的部分。

```
Problem
Concurrent transactions
需要 Serializability
        ↓

Naive Solution
Execution 全程持 distributed locks
        ↓

Failure
lock waiting
network coordination
deadlock
slow remote CPU involvement
        ↓

New Idea
Optimistic Execution
先读、计算、buffer writes
        ↓

New Failure
两个 writers 可能都认为旧 version 有效
        ↓

Mechanism
Commit 时 lock Write Set with CAS
        ↓

New Failure
transaction 可能基于 stale read
        ↓

Mechanism
Validate Read Set versions
        ↓

New Failure
primary expose committed data 后 crash，
backup 还没有 commit state
        ↓

Mechanism
COMMIT-BACKUP first
        ↓

New Failure
coordinator crash / partial commit
        ↓

Mechanism
durable participant logs
+ recovery protocol
        ↓

New Failure
machine failure / partition / stale RDMA target
        ↓

Mechanism
leases
+ precise membership
+ reconfiguration
        ↓

Final Design

Optimistic execution
+
write-set locking
+
read validation
+
replication-aware commit
+
recovery
```

---

# Part 34：最重要的完整 Mental Model

以后你看到 OCC，脑子里最好出现这张图：

```
                    Transaction
                         |
                         v
                 +---------------+
                 | Execute       |
                 |               |
                 | Read objects  |
                 | remember ver. |
                 | buffer writes |
                 +-------+-------+
                         |
                         v
              +---------------------+
              | Lock Write Set      |
              | expected versions   |
              +----------+----------+
                         |
                   serialization
                       point ★
                         |
                         v
              +---------------------+
              | Validate Read Set   |
              +----------+----------+
                         |
                 changed? |
                  +-------+-------+
                  |               |
                 yes              no
                  |               |
                ABORT             v
                          +---------------+
                          | Make durable  |
                          | on replicas   |
                          +-------+-------+
                                  |
                                  v
                          +---------------+
                          | Install       |
                          | new versions  |
                          | unlock        |
                          +---------------+
```

---

# Part 35：与 6.824 Lab 的关系

在当前 6.5840 课程安排中，Lecture 14 OCC 后进入 Sharded KV Lab；lab 本身重点仍包括 shard movement、Replication、failure 和 reconfiguration，而扩展建议还明确提到跨 shard transactions 可实现 2PC + 2PL。也就是说，课程不是要求你照抄 FaRM，而是在训练你理解这些机制之间的设计空间。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-shard1.html?utm_source=chatgpt.com)

如果你自己实现一个简化 OCC KV，最值得维护的 state 是：

```
type Object struct {
    Value   []byte
    Version uint64
    Locked  bool
}

type Tx struct {
    ReadSet  map[Key]Version
    WriteSet map[Key]Value
}
```

真正难的不是代码，而是几个 invariant：

```
1. version mutation 必须和 committed update 一致

2. write lock acquisition 必须原子检查 expected version

3. 获得 write locks 后必须 validate reads

4. abort 必须释放所有 acquired locks

5. partial commit 不能对外暴露

6. crash 之后 commit decision 必须可恢复
```

debugging 最好按 phase 打日志：

```
tx=123 EXECUTE
tx=123 LOCK X v7 OK
tx=123 LOCK Y v9 OK
tx=123 VALIDATE A v4 OK
tx=123 VALIDATE B expected=7 actual=8 FAIL
tx=123 ABORT
```

不要只打：

```
transaction failed
```

否则并发 bug 很难看。

---

# Part 36：五级自测

### Level 1 — Concept

T：

```
Read X(v5)
Read Y(v9)
Write Z
```

commit 时：

```
X=v5
Y=v10
```

即使：

```
Z lock success
```

T 能 commit 吗？

答案应该是：

```
不能
```

因为 Read Set validation 失败。

---

## Level 2 — Execution

```
T1:
Read X(v5)
Write X

T2:
Read X(v5)
Write X
```

两者都完成 execution。

T1 先获得 X lock。

问：

> T2 应该等待 T1 吗？

FaRM-style protocol：

```
No
```

lock CAS 失败：

```
T2 abort
```

因此避免 lock waiting/deadlock。

---

## Level 3 — Failure

```
T1:

LOCK
✓

VALIDATE
✓

COMMIT-BACKUP
✓

Coordinator crashes

COMMIT-PRIMARY
还未全部执行
```

问：

> 能不能简单地认为 transaction 一定 abort？

不能。

现在必须：

```
recovery protocol
```

根据 durable logs 恢复 decision。

---

## Level 4 — Counterexample

如果把顺序从：

```
LOCK
↓
VALIDATE
```

改成：

```
VALIDATE
↓
之后很久
↓
LOCK
```

而且不再重新验证，会怎么样？

可能：

```
T validates A=v5

T2 changes A→v6

T locks write object B

T commits based on old A
```

所以 serialization proof 被破坏。

---

## Level 5 — System Design

假设你设计一个 Kubernetes-like metadata store：

```
99% transactions
只更新不同 customer object

1%
会同时冲突
```

并且 workload：

```
read-heavy
short transaction
low contention
```

这是 OCC 很自然的候选场景。

如果变成：

```
所有 requests 都更新 global counter
```

那么 OCC 可能变成：

```
abort machine
```

你应该开始考虑：

```
serialization
queueing
sharding
escrow/counters
batching
```

而不是盲目 retry。

---

# Part 37：30 秒版本

如果面试官问：

> OCC / FaRM 这节课主要讲什么？

可以回答：

> Optimistic Concurrency Control 的核心是让 transaction 在 execution 阶段不持有长期锁，而是记录读取对象的版本、buffer writes，在 commit 时锁定 write set 并验证 read set。如果数据在执行期间发生冲突，就 abort/retry；如果 validation 成功，就可以选择一个 serialization point 并保证 serializability。FaRM 把这种 OCC 与 RDMA、primary-backup replication、durable transaction logs 和 failure recovery 结合起来，从而在低 contention 的内存型 distributed database 中提供 strict serializable transactions，同时减少 distributed coordination。

---

# Part 38：3 分钟版本

主线可以说成：

```
传统 distributed transaction 如果 execution 全程加锁，
network delay 会让 lock duration 很长。

所以 OCC 选择：
先乐观执行。

Transaction 读取 object 时记录：

value + version

write 不立即发布，
而是 buffer locally。

commit 时首先使用 expected version
锁住整个 write set。

这个时刻成为 serialization point。

然后重新验证 read-only read set：

如果任何 object version 变化，
说明 transaction 基于 stale data，
于是 abort。

如果 validation 成功，
说明 serialization point 时，
transaction 所依赖的所有值仍有效。

FaRM 接下来还必须处理 durability：

先把 commit information durable 到 backups，
再让 primaries install writes，
increment versions 并 unlock。

因此：
OCC 负责 concurrency correctness；
replication 负责 durability；
recovery protocol 负责 crashes；
membership protocol 负责 partition/reconfiguration。

OCC 在 low contention 很有效，
但 high contention、large read set、long transactions
会造成大量 abort/retry。
```

---

# Part 39：深入版本

```
Problem
    ↓
Distributed locking 太贵

Model
    ↓
crash-recovery
primary + f backups
RDMA
NVRAM
partial synchrony
non-Byzantine

Algorithm
    ↓
Execute
Read versions
Buffer writes
Lock write set
Validate read set
Commit backups
Commit primaries
Truncate

Invariant
    ↓
At serialization point:
every value transaction depended on
is still valid

Serialization Point
    ↓
all write locks acquired

Safety
    ↓
Committed transactions
strictly serializable

Liveness
    ↓
Eventually progresses
if network stabilizes,
required replicas exist,
and conflicts do not cause endless aborts

Failure Handling
    ↓
Durable participant logs
leases
membership
reconfiguration
transaction recovery

Trade-off
    ↓
Avoid waiting
in exchange for
possible wasted work and aborts
```

---

# Part 40：最终知识网络

把 Lecture 14 挂到你的知识树上：

```
                       Distributed Systems
                              |
          +-------------------+-------------------+
          |                                       |
     Fault Tolerance                         Transactions
          |                                       |
   Replication / Raft                    +--------+--------+
          |                              |                 |
 State Machine Replication          Atomic Commit    Concurrency Control
                                          |                 |
                                         2PC        +-------+-------+
                                                    |               |
                                                   2PL             OCC
                                                                    |
                                                                   FaRM
                                                                    |
                                          +-------------------------+------------------+
                                          |                         |                  |
                                    Version Validation          RDMA            Replication
                                          |                         |                  |
                                   Serialization Point         Fast Path       Primary/Backup
                                          |                                            |
                                  Strict Serializability                         Recovery Logs
```

如果要把这节课压缩成你整个 Distributed Systems mental model 里的**一句最关键的话**，就是：

> **OCC 把“避免冲突”变成了“允许 speculative concurrency，然后在 commit 点证明这个执行可以被放进一个合法的 serial history”；FaRM 又进一步展示了，这个 concurrency-control decision 必须和 Replication、Durability、Failure Recovery 一起设计，才能成为真正的 distributed transaction system。**