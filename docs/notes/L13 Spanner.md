## MIT 6.824 Lecture 13 — Spanner

先给你整节课的主线：

> **Spanner 想解决的问题是：在全球分布、分片、复制的数据库中，怎样同时实现跨分片 ACID Transaction、强一致性，以及不加锁的 consistent snapshot read。**

真正把 Spanner 和普通的：

```
Paxos + 2PC + MVCC
```

区别开的关键，是它引入了：

```
TrueTime
   ↓
把“现在几点的不确定性”显式暴露出来
   ↓
让 transaction timestamp
和现实世界时间建立可靠关系
   ↓
External Consistency
```

整节课可以压缩成一条链：

```
Replication
    ↓
Paxos

Sharding
    ↓
每个 shard / Paxos group 独立复制

跨 shard Transaction
    ↓
2PC

但：
2PC 只解决 atomic commit
并没有解决全局 transaction ordering
    ↓
给 transaction timestamp

但：
普通机器时钟不可靠
    ↓
TrueTime

但：
TrueTime 只给一个时间区间
    ↓
Commit Wait

最终：
Paxos + 2PC + MVCC + TrueTime
    ↓
Externally Consistent Distributed Database
```

---

# Part 1：Spanner 到底在解决什么问题？

如果只能记一个问题：

> **怎样让全球数据库里的事务拥有一个与现实世界顺序一致的全局 serialization order？**

假设有：

```
        US                           Europe

    Shard A                       Shard B
Alice=$1000                     Bob=$500
```

Alice 转账给 Bob：

```
T1:

Alice -= 100
Bob   += 100
```

因为涉及两个 shard：

```
              T1
             /  \
            /    \
           v      v
       Shard A  Shard B
```

你已经知道，可以用 2PC：

```
prepare A
prepare B

if both prepared:
    commit A
    commit B
```

于是可以保证：

```
要么：

Alice = 900
Bob   = 600

要么：

Alice = 1000
Bob   = 500
```

不会出现只扣钱不加钱。

所以：

> 2PC 解决 Atomicity。

但现在出现另一个问题。

---

### 一个更微妙的问题

假设：

```
Client A:

Transfer()
   |
   +--------------------> DB
                          commit
   <-------------------- OK


Client B:

                              ReadBobBalance()
                                   |
                                   v
```

现实世界中：

```
T1 已经成功返回

之后

T2 才开始
```

你自然希望数据库认为：

```
T1 < T2
```

于是 T2 应该看到 Bob=$600。

问题是：

```
T1 在美国执行
T2 在欧洲执行
```

如果我们简单使用本地 wall clock：

```
US clock = 10:00:00.120
EU clock = 10:00:00.080
```

那么可能：

```
T1 commit timestamp = 120
T2 timestamp        = 80
```

数据库认为：

```
T2 < T1
```

而现实世界：

```
T1 < T2
```

产生冲突。

---

## 为什么单机上没这么麻烦？

单机：

```
Transaction Manager
       |
       v
single clock
single log
single lock manager
```

所有事务都经过同一个地方。

你很容易定义：

```
T1 commit
T2 commit
T3 commit
```

顺序。

但分布式数据库：

```
       Shard A
       Paxos Group A
           |
           |
Client ----+----- Shard B
           |      Paxos Group B
           |
           +----- Shard C
                  Paxos Group C
```

没有天然的：

```
Global Sequencer
```

也没有绝对准确的：

```
Global Clock
```

---

## 最 naive 的方案

最容易想到：

```
timestamp = local wall clock
```

但 distributed clocks：

```
Machine A: 100
Machine B: 97
Machine C: 105
```

而且不断 drift。

于是：

```
现实：

T1 -------- finish
                  T2 -------- start


timestamp:

T1 = 100
T2 = 95
```

现实顺序和数据库顺序矛盾。

这正是 Spanner 要突破的地方。

---

# Part 2：它在整个 6.824 知识树里的位置

先从前面的知识一路推下来。

```
RPC
 ↓
机器之间如何通信

Threads / Concurrency
 ↓
同一节点如何处理并发

Replication
 ↓
一个节点挂掉，数据仍然存在

Consensus / Paxos / Raft
 ↓
多个 replica 对操作顺序达成一致

State Machine Replication
 ↓
复制的不只是 data
而是 deterministic state transitions

ZooKeeper
 ↓
利用 replication + consensus
提供 coordination service

Sharding
 ↓
一组机器放不下全部数据

Distributed Transaction
 ↓
一个 transaction 跨多个 shard

2PC
 ↓
保证跨 shard atomic commit

但是：
2PC 不解决全局真实时间顺序

                 ↓

              Spanner

Paxos + Sharding + 2PC + MVCC + TrueTime
```

这里几个概念一定要区分。

---

## Paxos / Raft vs 2PC

这是整个 Spanner 最重要的边界之一。

假设：

```
Shard A:

A1
A2
A3
```

这三个机器复制同一个 shard。

Paxos/Raft 回答：

> A1、A2、A3 应该按照什么顺序执行操作？

比如：

```
index 1: x=10
index 2: x=20
index 3: x=30
```

这是：

```
Replication agreement
```

---

现在：

```
Transaction T

Shard A
Shard B
Shard C
```

2PC 回答：

> T 在 A/B/C 上到底全部 commit 还是全部 abort？

这是：

```
Atomic commitment
```

因此：

```
Paxos / Raft
       ≠
      2PC
```

Spanner 使用的是：

```
每个 shard 内：
Paxos

多个 shard 之间：
2PC
```

可以画成：

```
                     Transaction
                         |
          +--------------+--------------+
          |              |              |
          v              v              v

       Shard A         Shard B        Shard C

      A1 A2 A3        B1 B2 B3       C1 C2 C3
        Paxos           Paxos          Paxos

          \              |              /
           \             |             /
            +----------- 2PC ---------+
```

---

## Serializability vs Linearizability

理解 Spanner 前必须把这个区别搞清楚。

假设两个 transaction：

```
T1:
x = 1

T2:
read x
```

Serializability 要求：

存在一个 serial order：

```
T1 → T2
```

或者：

```
T2 → T1
```

只要 execution 等价于其中一个即可。

---

但是它不一定关心真实时间。

现实可能是：

```
T1 finished
           T2 started
```

普通 Serializability 理论上仍可能选择：

```
T2 → T1
```

作为 serialization order。

---

Spanner 提供更强的：

## External Consistency

可以理解成：

> **Serializable，并且 serialization order 必须尊重 real-time order。**

也就是：

```
如果 T1 在现实世界已经完成
然后 T2 才开始

那么：

timestamp(T1) < timestamp(T2)
```

这也通常称为：

```
Strict Serializability
```

对于单个 object，这种 real-time ordering 的感觉非常接近：

```
Linearizability
```

所以可以建立：

```
Linearizability
    单个 operation/object 常用

Strict Serializability /
External Consistency
    transaction 范围
```

不要把它们和普通：

```
Serializable
```

混在一起。

---

# Part 3：Spanner 的核心 Mental Model

整节课我建议你只抓住 6 个东西：

```
1. Paxos Group

2. 2PC

3. MVCC

4. Commit Timestamp

5. TrueTime

6. Commit Wait
```

下面一个个拆。

---

## Concept 1：Paxos Group

### 解决的问题

一个 shard 不能只存在一份。

比如：

```
US-West
US-East
Europe
```

都有 replica。

必须回答：

> 哪个版本是真正的数据？

---

### 一句话定义

> 一个 Paxos group 是同一份数据的一组 replicas，通过 Paxos 对 mutation 顺序达成一致。

教学上你可以近似理解：

```
Shard ≈ Paxos Group
```

真实 Spanner 里的 tablet / directory / Paxos group 映射更复杂，但先这样理解足够。

---

例如：

```
         Paxos Leader
              |
        Put(x=100)
              |
       +------+------+
       |             |
       v             v
Replica B        Replica C
```

只要 quorum 接受：

```
mutation durable
```

leader 挂掉也不会丢。

---

## Concept 2：2PC

现在一个 transaction：

```
T1:

x on Shard A
y on Shard B
```

每个 shard 自己 Paxos 一致，并不能解决：

```
A commit
B abort
```

所以需要：

```
Two-Phase Commit
```

---

Happy path：

```
Coordinator

    prepare
      |
      +------> Shard A
      |
      +------> Shard B

A: prepared
B: prepared

      |
      v

Coordinator:
COMMIT

      |
      +------> A
      +------> B
```

但注意：

> Paxos 和 2PC 是嵌套关系。

例如 participant A 执行：

```
PREPARE T
```

不能只是写 Leader memory。

它通常要：

```
PREPARE
   ↓
Paxos replicate
   ↓
durable
```

然后才回复：

```
PREPARED
```

否则 participant leader crash 后：

```
“我刚才 prepare 了吗？”
```

都不知道。

---

## Concept 3：MVCC

Spanner 不只保存：

```
x = 100
```

而是：

```
x:

(timestamp=10, value=50)
(timestamp=20, value=80)
(timestamp=30, value=100)
```

也就是：

```
Multi-Version Concurrency Control
```

所以：

```
Read(x, timestamp=25)

→ value=80
```

---

这件事非常关键，因为有了 MVCC：

```
Writer 正在写最新值

Reader 想看过去快照
```

两者不一定冲突。

例如：

```
time →

T1 write x@100

                          T2 snapshot @80
```

T2 可以直接读：

```
x 的 <=80 最新版本
```

不必等 T1。

因此：

> TrueTime 给你一个 globally meaningful timestamp。

> MVCC 让你真正能够“在这个 timestamp 上读取数据库”。

两者组合非常漂亮。

---

## Concept 4：Commit Timestamp

Spanner 会给 committed transaction 一个 timestamp：

```
T1 → 100
T2 → 120
T3 → 150
```

这个 timestamp 不只是 metadata。

它定义：

```
transaction serialization order
```

也定义 MVCC version：

```
x@100
y@120
z@150
```

所以 timestamp 同时承担：

```
ordering
+
versioning
+
snapshot position
```

---

## Concept 5：TrueTime

这里进入 Spanner 最著名的机制。

普通 API：

```
time.Now()
```

给你：

```
10:00:00.123
```

看起来好像“我知道现在几点”。

实际上这是谎言。

真实情况可能是：

```
真实时间 ∈
[10:00:00.120,
 10:00:00.126]
```

Spanner 把不确定性显式暴露出来。

TrueTime：

```
TT.now()
```

返回：

```
[earliest, latest]
```

例如：

```
[100, 106]
```

含义：

> TrueTime 承诺真实 absolute time 一定位于这个区间。

不是：

```
现在大约 103
```

而是更强：

```
100 <= actual time <= 106
```

---

通常定义 uncertainty：

```
ε = (latest - earliest) / 2
```

例如：

```
TT.now() = [96, 104]

ε = 4ms
```

中间值：

```
100
```

只是估计值。

真正有价值的是：

```
bounded uncertainty
```

---

## TrueTime 怎么做到？

Spanner 论文里的关键基础设施包括：

```
GPS clocks
+
atomic clocks
+
time masters
+
regular synchronization
```

普通服务器会定期和这些 time master 同步。

随着距离上次同步时间增长：

```
clock uncertainty ↑
```

因此 TrueTime 不声称：

```
我的 clock 完全准确
```

而是声称：

```
我知道误差最大不会超过多少
```

这是根本性的思维转换。

---

## Concept 6：Commit Wait

这是很多人第一次学 Spanner 时最容易漏掉的地方。

假设：

```
TT.now() = [100, 110]
```

transaction 选择：

```
commit timestamp = 110
```

注意：

现在真正的 physical time 可能只有：

```
103
```

也就是说：

```
commit timestamp = 110
```

实际上还在“未来”。

那怎么办？

Spanner：

> 等。

直到能够确定：

```
real time > 110
```

也就是：

```
TT.after(110) == true
```

再向 client 宣布 commit 完成。

这个等待就是：

## Commit Wait

---

# Part 4：为什么 Commit Wait 是神来之笔？

假设：

```
T1 timestamp = s1 = 100
```

Spanner 不会立刻：

```
reply OK
```

而是等到：

```
actual time > 100
```

才返回。

所以当 client 收到：

```
T1 OK
```

我们已经知道：

```
s1 < real time
```

现在 T2 在之后才开始。

T2 coordinator 调：

```
TT.now()
```

例如：

```
[105, 115]
```

它选择：

```
s2 >= TT.now().latest
```

所以：

```
s2 >= 115
```

因此：

```
s1 < real time < s2
```

从而：

```
s1 < s2
```

所以：

```
T1 completes before T2 starts

        ↓

timestamp(T1) < timestamp(T2)
```

这就是 External Consistency 的核心。

---

## 最重要的 Correctness Proof

假设：

```
T1 → T2
```

其中：

```
T1 finishes
before
T2 starts
```

设：

```
T1 timestamp = s1
T2 timestamp = s2
```

因为 T1 commit wait：

```
T1 返回时：

real_time > s1
```

T2 在 T1 返回之后才开始。

所以 T2 选择 timestamp 时：

```
real_time > s1
```

而 TrueTime 保证：

```
TT.now().latest >= real_time
```

Spanner 选择：

```
s2 >= TT.now().latest
```

所以：

```
s2 > s1
```

因此：

```
real-time order
        ⇒
timestamp order
```

这就是整个 Spanner 时间设计最值得记的证明。

---

# Part 5：系统模型和 assumptions

### Node Model

Spanner 主要考虑：

```
Crash / recovery
```

而不是：

```
Byzantine
```

节点可能：

```
crash
restart
disk failure
network isolation
```

但不会考虑：

```
replica 恶意伪造 Paxos message
```

---

### Persistent State

重要数据通过：

```
Paxos log
+
replicated storage
+
MVCC versions
```

持久化。

例如：

```
PREPARED
COMMIT
transaction data
```

关键 durable decisions 必须 survive leader crash。

---

### Network

网络可能：

```
delay
loss
reordering
partition
```

系统不会假设普通 RPC：

```
一定成功
```

Consensus safety 不依赖固定 latency。

Liveness 通常依赖：

```
eventually enough replicas can communicate
```

所以 mental model 可以看成：

```
Safety:
asynchronous conditions 下仍要成立

Liveness:
需要 eventual synchrony / quorum connectivity
```

---

### Clock Model

这一点与很多 distributed algorithm 不同。

Spanner 有一个额外 assumption：

> TrueTime 给出的区间必须真实包含 absolute time。

也就是：

```
earliest <= real_time <= latest
```

这是 Spanner External Consistency proof 的基础。

如果 uncertainty 变大：

```
ε ↑
```

通常不是：

```
consistency ↓
```

而是：

```
commit wait ↑
latency ↑
```

这非常重要。

Spanner 的设计思想是：

> 宁可慢，也不要骗自己时间是准的。

但如果 TrueTime 的误差保证本身被破坏：

```
real time 不在 interval 中
```

那么 External Consistency 的时间证明就可能失效。

---

# Part 6：完整 Read-Write Transaction Happy Path

现在真正跑一个 transaction。

```
T:

update Account[Alice]
update Account[Bob]
```

假设：

```
Alice → Paxos Group A
Bob   → Paxos Group B
```

---

### Step 1：Client 读取

Client 先读取：

```
Alice balance
Bob balance
```

read-write transaction 通常需要参与 concurrency control。

简单理解：

```
Leader maintains lock table
```

防止 conflicting transaction 同时破坏 serializability。

---

### Step 2：Client buffer writes

假设得到：

```
Alice: 1000 → 900
Bob:    500 → 600
```

transaction 准备 commit。

---

### Step 3：选一个 participant 作为 coordinator

例如：

```
Group A Leader
```

承担 2PC coordinator。

于是：

```
Client
   |
   v
Coordinator A
   |
   +---- participant A
   |
   +---- participant B
```

---

### Step 4：Prepare

Coordinator：

```
PREPARE T
```

发给参与 groups。

每个 participant：

```
acquire required locks
        ↓
choose prepare timestamp
        ↓
replicate PREPARE through Paxos
        ↓
reply PREPARED
```

关键点：

```
PREPARED
```

不能只是：

```
leader RAM flag
```

否则 leader crash：

```
prepared state 消失
```

2PC 就不安全。

---

### Step 5：Coordinator 选择 commit timestamp

Coordinator 收到：

```
A prepared @ 120
B prepared @ 125
```

同时：

```
TT.now().latest = 130
```

它需要选择：

```
commit timestamp s
```

满足类似：

```
s >= prepare timestamps

s >= TT.now().latest

s > coordinator 已经分配的相关 timestamps
```

例如：

```
s = 130
```

---

### Step 6：记录 Commit

Coordinator：

```
COMMIT T @130
```

通过自己的 Paxos group 复制。

此时：

```
decision durable
```

---

### Step 7：Commit Wait

但还不能马上跟 client 说：

```
OK
```

因为真实时间可能：

```
125
```

而 commit timestamp：

```
130
```

于是等：

```
TT.after(130)
```

成立。

---

### Step 8：Participants commit

之后：

```
A writes version @130
B writes version @130
```

例如：

```
Alice:
@100 = 1000
@130 = 900

Bob:
@100 = 500
@130 = 600
```

release locks。

---

### Step 9：Client 收到 OK

这时候：

```
T committed @130
```

而且：

```
absolute real time >130
```

因此以后真正“发生在它之后”的 transaction，不会得到更小的 timestamp。

---

## 完整 timeline

```
time →

Client:
   begin
     |
     | reads
     |
     | commit
     v
Coordinator A:
     PREPARE ----------------------+
       |                           |
       |                           |
Participant A:                     |
     lock                          |
     Paxos PREPARE                 |
     ---- PREPARED ----------------+
                                   |
Participant B:
     lock
     Paxos PREPARE
     -------- PREPARED ------------+
                                   |
Coordinator:
                  choose s=130
                       |
                  Paxos COMMIT
                       |
                  commit wait
                  .............
                       |
                TT.after(130)
                       |
          +------------+-----------+
          |                        |
          v                        v
         A commit                 B commit

Client:
                             <---- OK
```

---

# Part 7：如果这里 crash 呢？

现在才进入真正的 distributed-system 思考。

---

## Failure 1：Participant leader 在 prepare 前 crash

例如：

```
A prepared
B leader crashes before prepared
```

Coordinator 收不到：

```
PREPARED B
```

transaction 无法 commit。

最终：

```
abort
```

或者等新的 B leader 恢复协议状态。

Safety 没问题。

Availability 受影响。

---

## Failure 2：Participant 回复 PREPARED 后 crash

这是 2PC 最经典的问题。

假设：

```
B:
Paxos PREPARE durable
        ↓
reply PREPARED
        ↓
crash
```

因为 PREPARE 已持久化：

```
new leader
```

可以知道：

```
T 已经 prepared
```

所以不能随便：

```
abort
```

必须等待 transaction outcome。

这就是：

```
2PC participant becomes uncertain
```

---

## Failure 3：Coordinator crash

经典 2PC 问题：

```
A: prepared
B: prepared

Coordinator:
???
crash
```

普通单机 coordinator 会造成：

```
blocking
```

Spanner 的改进在于：

```
Coordinator state itself
is replicated with Paxos
```

所以 coordinator leader crash：

```
new Paxos leader
```

通常可以恢复 durable transaction decision/state。

这让：

```
single coordinator machine failure
```

不等于永久阻塞。

但是：

> 这并不意味着 2PC 从理论上变成了 Consensus。

如果整个 coordinator Paxos group 无法获得 quorum：

```
transaction progress
```

仍然可能阻塞。

这是非常重要的边界。

---

## Failure 4：Network Partition

假设：

```
Region A |X| Region B
```

如果某个 Paxos group：

```
still has quorum
```

可以继续。

没有 quorum：

```
cannot commit new Paxos entries
```

所以：

```
Safety preserved
Availability lost
```

符合我们前面反复学习的：

```
partition
    ↓
cannot safely invent agreement
```

---

## Failure 5：Commit message 丢失

例如：

```
Coordinator → Participant B

COMMIT @130

packet lost
```

没有关系。

因为：

```
commit decision durable
```

协议可以 retry。

Participant 必须使：

```
duplicate COMMIT
```

成为幂等操作。

最终：

```
B learns outcome
```

---

# Part 8：Read-only transaction 为什么特别漂亮？

现在来看 Spanner 的另一半。

假设：

```
Analytics Query:

read Account A
read Account B
read Account C
```

如果我们使用锁：

```
reader locks A/B/C
```

那么：

```
writers block
```

对于全球分析查询非常糟糕。

---

MVCC 给了另一种办法：

```
choose snapshot timestamp = s
```

然后：

```
read all values as of s
```

比如：

```
snapshot s=100
```

每个 shard：

```
Shard A → value <=100
Shard B → value <=100
Shard C → value <=100
```

于是得到：

```
one consistent database snapshot
```

---

### 为什么不需要 read locks？

假设：

```
Reader:
snapshot @100

Writer:
commits @120
```

Reader 根本不需要阻止 writer。

因为它永远读：

```
<=100
```

的版本。

所以：

```
Reader ---------- read x@100

Writer                 write x@120
```

两者可以并发。

这就是：

```
MVCC
```

最核心的价值之一。

---

## 但是这里有一个新问题

Replica A 可能已经执行到：

```
timestamp 150
```

Replica B 只执行到：

```
timestamp 90
```

现在你要求：

```
snapshot @100
```

A 可以回答。

B 不行。

因为 B 还不知道：

```
timestamp <=100
```

是不是还有未来才会到达的 committed transaction。

因此 replica 需要判断：

> 我是否已经安全地知道 timestamp s 之前所有应该可见的东西？

这就出现：

## Safe Time

可以粗略理解：

```
t_safe
```

表示：

> 对于 ≤ t_safe 的 timestamp，我已经不会再遗漏新的、更早版本。

所以：

```
snapshot timestamp <= t_safe

→ 可以安全读
```

否则：

```
wait
```

---

## Safe Time 的直觉

例如 Replica B 当前有：

```
x@80
```

你问：

```
Read(x,100)
```

B 不能仅仅因为：

```
目前最新 version=80
```

就返回。

可能有一个 transaction：

```
timestamp=95
```

已经 commit，但 replication message 还在路上。

如果 B 现在返回：

```
x@80
```

快照就错了。

因此 replica 必须确认：

```
不会突然再来一个 <=100 的合法 commit
```

这就是 safe timestamp / safe time 的意义。

---

# Part 9：TrueTime 到底解决什么，不解决什么？

这是 Top 级误区。

TrueTime：

```
不是 Consensus
不是 Replication
不是 Atomic Commit
不是 MVCC
```

TrueTime 只提供：

> 带有明确误差边界的 wall-clock information。

所以：

```
Paxos
→ agreement

2PC
→ atomic commit

Locks
→ read-write serializability

MVCC
→ historical versions

TrueTime
→ real-time timestamp ordering

Commit Wait
→ external consistency proof
```

它们各有边界。

---

# Part 10：为什么不用 Lamport Clock？

你已经学过 Lamport Clock，所以这个问题非常重要。

Lamport Clock 可以保证：

```
A → B
    ⇒
L(A) < L(B)
```

这里：

```
→
```

是 happens-before。

但假设：

```
T1 在美国完成

之后一个完全独立的 client

在欧洲创建 T2
```

欧洲机器可能完全没收到：

```
T1 的 Lamport timestamp
```

于是它不知道：

```
T1 < T2
```

现实世界顺序没有自动传播成：

```
causal message edge
```

---

TrueTime 则通过：

```
physical-time interval
```

让两个彼此没通信过的节点也能建立：

```
real-time ordering
```

因此：

```
Lamport Clock:
logical causality

TrueTime:
bounded physical time
```

两者不是谁取代谁。

解决的问题不同。

---

## Vector Clock 呢？

Vector Clock 更擅长回答：

```
A happened-before B？

还是：

A || B concurrent？
```

但它的 metadata：

```
O(number of participants)
```

而且仍然没有：

```
actual wall-clock order
```

所以不是 Spanner 需要的 abstraction。

---

# Part 11：为什么不用一个 Global Timestamp Oracle？

另一种 naive design：

```
             Global Timestamp Server
                       |
          +------------+------------+
          |            |            |
        Region A     Region B     Region C
```

每个 transaction：

```
GET /nextTimestamp
```

返回：

```
100
101
102
103
```

这样当然可以建立 total order。

但代价：

```
global coordination
+
cross-region latency
+
potential bottleneck
+
availability dependency
```

Spanner 的目标之一就是避免：

> 每个 transaction 都为了 timestamp 去一个全球 centralized sequencer。

TrueTime 利用 physical clocks：

```
让各个 region 能独立生成时间相关 timestamp
```

只支付：

```
uncertainty → commit wait
```

这是非常漂亮的 trade-off：

```
global communication
        vs
bounded clock uncertainty
```

Spanner 选择后者。

---

# Part 12：State 和 Invariants

我们把重要状态按层次分。

### Paxos Group State

概念上：

```
log[]
leader
replicas
applied position
```

作用：

```
replicated durable ordering
```

关键状态必须持久化。

---

### Transaction State

例如：

```
transaction ID
participants
locks
prepare state
prepare timestamp
commit timestamp
outcome
```

其中：

```
PREPARED / COMMITTED
```

这类影响 recovery 的状态必须通过 durable replicated state 保存。

---

### MVCC State

例如：

```
key = user/123

versions:
   (80, Alice)
   (100, Alice2)
   (150, Alice3)
```

每个 version：

```
(timestamp, value)
```

---

## 最重要的 Invariants

### Invariant 1

同一 transaction：

```
所有 participant
```

最终必须得到同一个：

```
commit / abort decision
```

否则 Atomicity 崩溃。

---

### Invariant 2

如果：

```
T1 finishes before T2 starts
```

那么必须：

```
timestamp(T1) < timestamp(T2)
```

这是 External Consistency。

---

### Invariant 3

snapshot @s：

```
必须看到所有 timestamp <= s
且应该可见的 commits
```

同时：

```
不能看到 timestamp > s
```

否则不是 consistent snapshot。

---

### Invariant 4

同一个 Paxos log position：

```
不能有两个不同 mutation 都被 chosen
```

这是 Consensus 层提供的基础。

---

# Part 13：Safety vs Liveness

### Safety

Spanner 最核心的 Safety 包括：

```
Atomicity

Serializability

External Consistency

Replica Agreement

Snapshot Consistency
```

Safety 的意思：

> 即使系统很倒霉，也不能返回错误结果。

比如 network partition：

```
宁可拒绝 write
```

也不能：

```
两个 partition 分别 commit 冲突状态
```

---

## Liveness

Liveness 是：

> 在环境恢复正常后，事务最终能够向前推进。

它通常需要：

```
Paxos majority reachable

Coordinator group majority reachable

participants reachable

network eventually delivers messages

TrueTime functioning within assumptions
```

如果这些条件不满足：

```
Spanner 可以停
```

但：

```
不应该撒谎
```

这是典型：

```
Safety over availability
```

场景。

---

# Part 14：Failure Matrix

|Failure|系统行为|Safety|Availability|核心机制|
|---|---|---|---|---|
|一个 replica crash|其他 replica 继续|保持|通常保持|Paxos quorum|
|Paxos leader crash|选新 leader|保持|暂时下降|Paxos|
|少数 replica 失联|quorum 继续|保持|可继续|Majority|
|多数 replica 失联|停止新 commit|保持|丢失|Paxos|
|Participant 在 prepare 后 crash|新 leader 恢复状态|保持|暂时下降|durable prepare|
|Coordinator leader crash|coordinator Paxos group 恢复|保持|暂时下降|replicated coordinator|
|整个 coordinator group 无 quorum|transaction 可能阻塞|保持|丢失|2PC limitation|
|Commit RPC 丢失|retry|保持|最终恢复|idempotency|
|Duplicate RPC|重复处理必须无害|保持|保持|txn ID / durable state|
|Clock uncertainty 变大|commit wait 增长|保持|latency 下降|TrueTime|
|TrueTime error bound 被破坏|real-time proof 可能失败|可能破坏|不确定|fundamental assumption|

注意最后一行。

这是 Spanner 少数真正依赖物理世界基础设施的地方。

---

# Part 15：五个最容易误解的地方

### ❌ 1. TrueTime 让所有服务器的 clock 完全同步

错。

TrueTime 的设计恰恰承认：

```
clock 不完全同步
```

它提供：

```
[earliest, latest]
```

而不是伪装成：

```
exact now
```

真正关键的是：

```
bounded uncertainty
```

---

### ❌ 2. TrueTime 自己提供 Linearizability

错。

TrueTime 只是时间 primitive。

External Consistency 来自多个机制组合：

```
Concurrency Control
+
Paxos
+
2PC
+
timestamp assignment
+
TrueTime
+
commit wait
```

---

### ❌ 3. Paxos 已经解决 Distributed Transaction

错。

Paxos：

```
replicas agree
```

2PC：

```
participants atomically commit
```

比如：

```
A1/A2/A3
```

用 Paxos。

但：

```
Shard A + Shard B
```

仍然需要 transaction protocol。

---

### ❌ 4. 2PC + Paxos 就自动得到 External Consistency

错。

它们能让：

```
transaction atomic
+
data replicated
```

但：

```
real-time ordering
```

仍然没有解决。

TrueTime + timestamp protocol 才补上这一层。

---

### ❌ 5. Snapshot Read 就是从 follower 随便读

这是工程里特别容易犯的错。

如果 follower 落后：

```
Leader:
x@100

Follower:
x@80
```

你不能：

```
Read@100
```

直接在 follower 返回 x@80。

必须确认：

```
replica safe for timestamp 100
```

所以：

```
Follower Read
≠
Automatically Consistent Read
```

---

# Part 16：Spanner Paper 的真正 Key Insight

2012 年的 Spanner paper 不只是：

> “Google 做了一个全球数据库。”

它最重要的思想组合是：

```
Global-scale sharded database

        +

Synchronous replicated state

        +

Distributed transactions

        +

Multi-version storage

        +

Clock uncertainty as an explicit API
```

其中最有辨识度的创新是：

## TrueTime + Commit Wait

也就是：

> 与其试图制造完美 clock，不如把 clock error 明确量化出来，然后在算法里补偿它。

这是一个非常深的 distributed-system design pattern：

```
无法消除 uncertainty
       ↓
measure uncertainty
       ↓
expose uncertainty
       ↓
design protocol around it
```

---

## Paper Problem

以前经常存在一种 trade-off：

```
Global scale
Strong transaction semantics
High availability
Good read performance
```

很难全部兼得。

Spanner 希望提供：

```
全球数据放置
+
synchronous replication
+
distributed transaction
+
strong consistency
+
snapshot reads
```

---

## Previous Approaches

大致两类。

第一类：

```
traditional relational DB
```

事务语义强，但是：

```
global geo-distribution
```

困难。

第二类：

```
Dynamo-style distributed KV
```

geo-distribution 强，但是很多早期系统：

```
eventual consistency
```

事务能力有限。

Spanner 试图把两个世界合起来。

---

## Design

逻辑上：

```
                 Spanner Universe

       +-------------+-------------+
       |                           |
      Zone A                      Zone B
       |                           |
   Spanservers                 Spanservers
       |                           |
    Tablets                      Tablets
       |
    Paxos Groups
```

data：

```
sharded
```

每个 shard：

```
replicated
```

跨 shard：

```
transaction
```

历史版本：

```
MVCC
```

全球 timestamp semantics：

```
TrueTime
```

---

## Evaluation 应该怎么看？

不要只看 paper 的：

```
requests/sec
```

更应该看设计验证了哪些 hypothesis。

大致包括：

```
replication latency

transaction latency

read-only transaction performance

scaling with servers/data

TrueTime uncertainty

commit-wait overhead

availability / failure recovery
```

特别值得关注的是：

> TrueTime uncertainty 会直接进入 commit latency。

粗略理解：

```
write latency
≈
replication / coordination latency
+
commit wait
```

如果：

```
ε ↑
```

那么：

```
commit wait ↑
```

这是明确的工程 trade-off。

---

## What aged well?

非常多。

今天仍然极其重要的思想：

```
MVCC snapshot reads

sharded replicated databases

per-range consensus groups

2PC over consensus-backed shards

strong transaction semantics

clock uncertainty awareness

external consistency
```

这些思想在现代 distributed SQL 系统里都能看到影子。

---

## What changed?

现代系统不一定复制 Spanner 的：

```
TrueTime implementation
```

因为普通公司没有：

```
全球 GPS / atomic clock infrastructure
```

所以一些数据库会使用：

```
Hybrid Logical Clock
HLC
```

或者 timestamp oracle 等其他技术。

也就是说：

```
目标相同

real-time-ish global ordering

实现不同
```

---

# Part 17：Spanner vs CockroachDB

非常值得建立这个 mental model。

Spanner：

```
TrueTime
[earliest, latest]
```

然后：

```
commit wait
```

---

CockroachDB 等现代系统常使用：

```
Hybrid Logical Clock
```

粗略形式：

```
(physical_time, logical_counter)
```

物理 clock 给大致时间。

logical component 解决：

```
同一 physical timestamp
+
clock skew / causal ordering
```

它们也需要面对：

```
clock uncertainty
```

只是协议设计不同。

所以不要形成：

```
distributed SQL = TrueTime
```

而应该理解：

> 所有强一致全球数据库都必须解决 transaction ordering，但解决方式不唯一。

---

# Part 18：和 PostgreSQL / MySQL 的关系

单机 PostgreSQL：

```
Transaction Manager
Lock Manager
WAL
MVCC
```

已经拥有：

```
ACID
MVCC
Snapshot
```

Spanner 真正增加的复杂度是：

```
机器不再是一个 fault domain

        ↓

WAL
→ Paxos replicated log

Local transaction
→ Distributed 2PC

Local clock/order
→ distributed timestamp ordering

Local MVCC
→ globally meaningful MVCC timestamp
```

可以把 Spanner 看成：

```
Database ideas
+
Distributed Systems ideas
```

的非常典型融合。

---

# Part 19：和你熟悉的 Kubernetes / etcd 联系

这里可以做一个很有用但要小心的类比。

etcd：

```
Raft
 ↓
globally ordered revision
```

例如：

```
revision 100
revision 101
revision 102
```

Kubernetes API Server：

```
write object
   ↓
etcd
   ↓
revision
```

这里已经有很强的：

```
ordered replicated state
```

---

但 etcd 和 Spanner 的规模模型不同。

etcd 更像：

```
一个 Raft group
```

保存 control-plane metadata。

而 Spanner：

```
thousands/millions of partitions
       ↓
many Paxos groups
       ↓
transactions across groups
```

所以：

```
etcd:
one consensus group solves global order
```

而 Spanner：

```
cannot put all data
through one consensus group
```

这就是 sharding 后系统复杂度突然爆炸的原因。

---

## Cloud Control Plane 类比

假设你的 cloud control plane：

```
Project
VPC
Cluster
NodePool
```

如果全部放同一个 strongly consistent DB：

```
Transaction:
create cluster
+
reserve CIDR
+
create quota record
```

单机 DB 很简单。

如果 metadata sharded：

```
Project DB shard
Network shard
Cluster shard
```

突然：

```
create cluster
```

就是 distributed transaction。

这和 Spanner 的核心问题完全一致。

---

## Terraform 类比

Terraform：

```
Desired State
      ↓
Plan
      ↓
Apply
```

和 Spanner 没有直接算法等价关系。

但有一个很重要的共同点：

```
“状态”
```

必须有明确版本和 ordering。

比如：

```
State version 100
State version 101
```

如果两个 writer 同时写：

```
lost update
```

所以 Terraform backend 通常也需要：

```
locking
+
versioning
```

不过它并不提供 Spanner 那种全球 distributed transaction semantics。

所以这个类比只能帮助理解 concurrency control，不能继续推太远。

---

## Kafka 类比

Kafka partition：

```
offset 100
offset 101
offset 102
```

很像：

```
single consensus/replication group ordering
```

但：

```
Partition A offset 100
Partition B offset 100
```

天然没有 global order。

这和 Spanner shard 面临的问题类似：

```
local order easy
global order hard
```

Spanner 通过 transaction timestamp 建立跨 shard order。

Kafka 通常并不试图给所有 partitions 一个全球 linearizable order。

---

## Redis 类比

单个 Redis primary：

```
commands serially executed
```

所以 local ordering 很简单。

但：

```
multi-region
+
async replica
```

后：

```
read follower
```

可能 stale。

这正好帮助理解为什么 Spanner read-only snapshot 不能：

```
“随便找 follower 读”
```

而需要：

```
safe timestamp
```

---

# Part 20：为什么这节课必须放在 2PC 后面？

因为如果你还没理解：

```
Atomic Commit
```

Spanner 会像一大团机制。

实际上可以按层搭：

```
Layer 1

Shard A:
Paxos

Shard B:
Paxos

        ↓

Layer 2

Transaction across A+B:
2PC

        ↓

Layer 3

What timestamp does transaction get?

        ↓

Layer 4

How to guarantee timestamp
respects real world?

        ↓

TrueTime + commit wait

        ↓

Layer 5

How do we exploit timestamp?

        ↓

MVCC + snapshot reads
```

你应该把 Spanner 看成：

> 前面几节课机制的组合，而不是完全新的独立协议。

---

# Part 21：为什么 2PC 还是会 blocking？

这是一个值得单独再强调一次的问题。

有人会产生：

```
“Coordinator 用 Paxos 复制了，
那 2PC 就 non-blocking 了。”
```

并不完全对。

考虑：

```
Coordinator Paxos Group:

C1
C2
C3
```

如果：

```
C1,C2 unreachable
```

只剩：

```
C3
```

没有 quorum。

那么 participant：

```
A = prepared
B = prepared
```

可能仍然无法安全确定：

```
COMMIT?

ABORT?
```

不能自己猜。

所以：

```
Paxos replication
```

大幅降低：

```
single coordinator failure
```

造成的 blocking。

但不能消除：

```
fundamental quorum availability requirement
```

---

# Part 22：为什么 Read-only transaction 不需要 2PC？

这是 Spanner 特别重要的性能设计。

read-only transaction：

```
does not mutate state
```

所以没有：

```
partial commit
```

问题。

只要选择一个：

```
timestamp s
```

然后：

```
Shard A read @s
Shard B read @s
Shard C read @s
```

即可。

所以它不需要：

```
2PC
```

也通常不需要：

```
read locks
```

这使：

```
global analytics / read-heavy workload
```

性能非常好。

---

# Part 23：一个完整例子

假设数据库：

```
Shard A:
Alice account

Shard B:
Bob account

Shard C:
Transaction history
```

初始：

```
@100

Alice = 1000
Bob   = 500
History=[]
```

T1：

```
transfer 100
```

涉及三个 shard。

执行：

```
A prepare
B prepare
C prepare
```

prepare timestamps：

```
A = 115
B = 118
C = 116
```

TrueTime：

```
TT.now() = [117,120]
```

Coordinator 选择：

```
commit timestamp = 120
```

然后：

```
Paxos COMMIT @120
```

等待：

```
TT.after(120)
```

之后：

```
Alice@120 = 900
Bob@120   = 600
History@120 = transfer...
```

---

随后 T2 做：

```
read-only snapshot @120
```

它读取：

```
A <=120
B <=120
C <=120
```

得到：

```
Alice=900
Bob=600
History contains transfer
```

不会：

```
Alice=900
Bob=500
```

因为三个 shard 都在：

```
same snapshot timestamp
```

上读取。

这就是：

```
Atomic Transaction
+
MVCC
+
Snapshot Read
```

组合后的力量。

---

# Part 24：最关键的一条 timeline

把整个课程浓缩进这个图：

```
real time →

T1:
     execute
        |
        | prepare
        v
    [A prepared]
    [B prepared]
        |
        v
choose commit timestamp = 100
        |
Paxos commit @100
        |
        | real time maybe 96
        |
        | commit wait
        | ..............
        |
        | actual time >100
        v
     reply OK
        |
        |
        | T2 starts AFTER OK
        v

T2:
     TT.now() = [105,110]

     choose timestamp >=110

     therefore:

     timestamp(T2) > timestamp(T1)
```

如果你能自己从这个 timeline 推导：

```
External Consistency
```

这节课就真正懂了。

---

# Part 25：和 Snapshot Isolation 的区别

另一个很容易混淆的地方。

Snapshot Isolation：

```
每个 transaction
从某个 consistent snapshot 读取
```

通常还避免某些 write-write conflicts。

但：

```
Snapshot Isolation
```

并不自动等于：

```
Serializability
```

经典问题：

```
Write Skew
```

仍然可能发生。

Spanner 的 read-write transaction 使用：

```
pessimistic concurrency control
+
locks
```

来实现更强的 transaction serializability。

而：

```
MVCC snapshot
```

主要帮助：

```
read-only transaction
```

和 historical reads。

所以：

```
MVCC
≠
Snapshot Isolation
≠
Serializable
```

MVCC 是 mechanism。

Snapshot Isolation 是 isolation semantics。

Serializability 是更强的 correctness property。

---

# Part 26：公式和 TrueTime

TrueTime：

```
TT.now() = [t_earliest, t_latest]
```

含义：

```
t_earliest
    <=
real physical time
    <=
t_latest
```

假设：

```
TT.now() = [96ms,104ms]
```

你不能说：

```
现在就是100ms
```

只能说：

```
真实时间一定在96~104之间
```

---

uncertainty：

```
ε =
(t_latest - t_earliest) / 2
```

代进去：

```
ε =
(104 - 96) / 2
= 4ms
```

所以大致：

```
estimate = 100
error <= 4ms
```

---

如果：

```
ε = 1ms
```

commit wait 通常较短。

如果：

```
ε = 20ms
```

可能需要更长等待。

所以：

```
better clock synchronization
        ↓
smaller uncertainty
        ↓
lower transaction latency
```

这是 Spanner 中一个非常直接的：

```
hardware/infrastructure quality
       ↓
database latency
```

联系。

---

# Part 27：与 6.824 Lab 的关系

Spanner 本身通常不是让你完整实现一个 Lab。

但前面 Lab 学到的东西几乎都是它的积木。

```
Raft Lab
   ↓
replicated shard

KV Raft
   ↓
database operations over replicated state machine

Snapshot
   ↓
log compaction / state persistence

Shard KV
   ↓
partitioning + ownership movement

Spanner
   ↓
imagine adding:
2PC
MVCC
global timestamps
TrueTime-like semantics
```

尤其你写 Raft Lab 时的：

```
term
log
commitIndex
apply
```

解决的是：

```
single replicated group's state
```

而 Spanner 的难点已经变成：

```
many replicated groups
+
transaction across them
```

这是一个重要能力升级：

```
Replication problem
        ↓
Composition of replicated systems
```

---

# Part 28：Problem → Solution Chain

这是我最建议你记住的总结。

```
Problem 1

单机数据库挂掉怎么办？
        ↓
Replication
        ↓
Paxos


Problem 2

一个 group 放不下全球数据
        ↓
Sharding


Problem 3

transaction 跨多个 shards
        ↓
2PC


Problem 4

每个 shard 都有自己的 ordering
没有 global transaction order
        ↓
Commit Timestamp


Problem 5

不同机器 wall clock 不一致
        ↓
TrueTime interval


Problem 6

TrueTime 仍存在 uncertainty
timestamp 可能在“未来”
        ↓
Commit Wait


Problem 7

怎样高效 consistent read？
        ↓
MVCC


Problem 8

怎样跨 shards 读同一个 snapshot？
        ↓
read at same timestamp


Problem 9

replica 可能落后
        ↓
Safe Time


最终：

Paxos
+
Sharding
+
2PC
+
Locks
+
MVCC
+
TrueTime
+
Commit Wait

        ↓

Spanner
```

---

# Part 29：30 秒版本

如果面试官问：

> Spanner 最核心的东西是什么？

可以回答：

> Spanner 是 Google 的 globally distributed transactional database。数据被 sharding，每个 shard 通过 Paxos replication，跨 shard read-write transaction 使用 2PC。Spanner 使用 MVCC 给数据版本打 timestamp，并通过 TrueTime 暴露 physical clock 的 bounded uncertainty。事务选择一个 commit timestamp，然后通过 commit wait 保证在向 client 返回之前，真实时间已经超过该 timestamp，因此如果一个 transaction 在现实世界中先完成，后开始的 transaction 一定获得更大的 timestamp。这使 Spanner 能提供 External Consistency，同时支持基于 timestamp 的无锁 consistent snapshot reads。

---

# Part 30：3 分钟版本

Spanner 首先面对的是：

```
全球数据库
+
sharding
+
replication
```

每个 shard 本身通过 Paxos 保证：

```
replica agreement
+
fault tolerance
```

如果 transaction 只访问一个 shard：

```
Paxos group 内解决
```

如果 transaction 跨多个 shard，则通过：

```
2PC
```

保证所有 shards：

```
all commit
or
all abort
```

但这只解决 Atomicity，还没有解决全球 transaction ordering。

Spanner 给 transaction 分配 commit timestamp，并使用 MVCC 把 timestamp 作为 database version。

问题在于：

```
distributed wall clocks 不完全同步
```

于是 Google 引入 TrueTime：

```
TT.now()
→ [earliest, latest]
```

这个 interval 保证真实时间位于其中。

transaction commit 时选择一个足够大的 timestamp，比如至少：

```
>= TT.now().latest
```

然后不能立刻返回，而是进行：

```
commit wait
```

直到：

```
TT.after(commitTimestamp)
```

成立。

因此当 T1 向 client 返回时：

```
real time > timestamp(T1)
```

如果 T2 在这以后才开始，它会选择：

```
timestamp(T2)
>= current TrueTime.latest
> timestamp(T1)
```

所以 database order 尊重 real-time order。

这就是：

```
External Consistency
```

同时，由于有 MVCC，read-only transaction 可以选择一个 timestamp，并在所有 shards 读取这个 timestamp 的 snapshot，而无需加入 2PC 或阻塞 writer。

---

# Part 31：深入版本

可以把 Spanner 记成八层：

```
Problem
Global sharded replicated DB

        ↓

Model
Crash/recovery
network partition
Paxos quorum
bounded clock uncertainty

        ↓

Replication
Paxos per shard

        ↓

Atomicity
2PC across Paxos groups

        ↓

Concurrency
pessimistic locking for read-write txn

        ↓

Versioning
MVCC

        ↓

Ordering
TrueTime-backed commit timestamp

        ↓

Correctness
commit wait
→ External Consistency

        ↓

Read scalability
snapshot timestamp
+
safe replicas

        ↓

Trade-off
strong consistency
in exchange for:
coordination latency
quorum dependency
commit-wait latency
```

---

# Part 32：最后的知识网络

把 Spanner 挂到整棵树上：

```
                         Distributed Systems
                                |
        +-----------------------+----------------------+
        |                                              |
   Fault Tolerance                               Transactions
        |                                              |
   Replication                                      ACID
        |                                              |
 Consensus / Paxos                              Serializability
        |                                              |
 State Machine Replication                         2PC
        |                                              |
        +------------------+---------------------------+
                           |
                           v
                        Spanner
                           |
          +----------------+----------------+
          |                |                |
       Sharding          MVCC            TrueTime
          |                |                |
     many Paxos       Versioned Data    Clock Interval
       Groups              |                |
          |          Snapshot Reads      Commit Wait
          |                |                |
          +----------------+----------------+
                           |
                           v
               External Consistency
                           |
                   Strict Serializability
```

---

### 最后真正应该形成的 Mental Model

不要把 Spanner 记成：

```
“Google 发明了一个很准的时钟。”
```

真正应该记成：

```
Distributed database
无法天然获得 global order

        ↓

Consensus
只能解决一个 replication group 的 order

        ↓

2PC
只能解决跨 groups atomicity

        ↓

所以需要 transaction timestamp

        ↓

但 physical clocks 有 uncertainty

        ↓

Spanner 不隐藏 uncertainty
而是显式暴露它：

[earliest, latest]

        ↓

选择 conservative timestamp

        ↓

通过 commit wait
等待现实时间追上 timestamp

        ↓

于是 transaction timestamp
能够尊重 real-world order

        ↓

再配合 MVCC

        ↓

既有强 transaction semantics
又有 scalable snapshot reads
```

其中整篇 Spanner 最值得你长期记住的一句话是：

> **TrueTime 的创新不是“让时间精确”，而是“知道时间有多不精确”，然后让数据库协议围绕这个 uncertainty 构造 correctness。**

这也是 Spanner 从一堆已有组件——Paxos、2PC、MVCC——跃迁成一个非常漂亮的 distributed database design 的关键。