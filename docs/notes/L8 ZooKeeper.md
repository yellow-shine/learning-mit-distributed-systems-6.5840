## MIT 6.824 Lecture — ZooKeeper

先把整节课压缩成一句话：

> **Raft/Paxos 解决的是“复制节点之间如何达成一致”，ZooKeeper 解决的是“普通分布式应用如何把这种一致性能力变成 Leader Election、Membership、Configuration、Distributed Lock 等可复用的 coordination primitive”。**

ZooKeeper 最值得学的地方，其实不是某个 API，而是一种系统设计思想：

```
不要让每个 distributed application
自己重新实现 Consensus / Failure Detection / Ordering。

把最困难的一小块：
    ordered metadata
    session
    ephemeral state
    conditional update
    notification

做成一个高度可靠的 coordination kernel。

复杂的 coordination protocol
再在 client side 组合出来。
```

这就是整节课的主线。

---

# Part 1：为什么 ZooKeeper 必须出现？

假设你有一组 Controller：

```
                 Cloud Control Plane

                  +-----------+
                  | API / DB  |
                  +-----+-----+
                        |
           +------------+------------+
           |            |            |
           v            v            v
       Controller A Controller B Controller C
```

为了 HA，你肯定不想只运行一个 Controller。

但是假设某项操作：

```
AllocateSubnet()
CreateLoadBalancer()
AssignShard()
RebalancePartition()
```

一次只能有一个 Controller 执行。

那么马上遇到问题：

```
谁是 Leader？
```

单机中这个问题几乎不存在：

```
bool isLeader = true;
```

因为只有一个进程。

分布式环境里却变成：

```
A            B            C

leader?

网络可能断
机器可能 crash
机器可能暂停
RPC 可能 timeout
旧 leader 可能并没有真正死亡
```

最 naive 的方案：

```
DB:

leader = A
```

A 不响应之后：

```
B:
    if timeout(A):
        leader = B
```

但是：

```
time →

A: leader -------- network partition -------------------
                        \
                         \ 仍然可以访问 Cloud API
                          \
B: -------- timeout A
           |
           +-- DB leader = B
           |
           +-- start working
```

现在：

```
A认为自己还是 leader
B认为自己已经是 leader
```

这就是经典的 **split-brain / stale leader**。

而 timeout 并不能证明 A 已经死亡：

```
timeout
   ≠
proof of failure
```

这时你可能说：

> 那我实现 Raft。

理论上当然可以。

但现在每一个需要：

```
Leader Election
Configuration
Membership
Distributed Lock
Barrier
Service Discovery
```

的系统都得：

```
实现一个 replicated log
实现 leader election
处理 disk persistence
处理 network partition
处理 crash recovery
处理 session
处理 retry
处理 duplicate RPC
```

这是极其昂贵的。

ZooKeeper 的问题意识因此是：

> **能不能把 hardest distributed-systems machinery 做一次，然后给所有应用提供一个极小但足够强大的 coordination API？**

ZooKeeper 论文把它描述为一个 **coordination kernel**，而不是在服务端硬编码 Leader Election、Lock、Queue 等每一种高级 primitive；应用利用少数底层 primitive 自己组合这些协议。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 2：它在 6.824 知识地图中的位置

你刚刚学完 Raft，那么现在出现 ZooKeeper 非常合理。

```
RPC / Threads
     ↓
distributed execution

Replication
     ↓
副本可以提高 fault tolerance
但副本之间可能产生分歧

Consensus
     ↓
决定一个 value / operation

Raft / Paxos
     ↓
构造 replicated ordered log

State Machine Replication
     ↓
所有 replica 按相同顺序执行 command
     ↓
得到 fault-tolerant replicated service

              ↓

          ZooKeeper

“How do applications USE this?”

              ↓

Configuration
Membership
Leader Election
Distributed Lock
Barrier
Naming
Coordination
```

这里一定要把几个东西分开。

### Raft vs ZooKeeper

Raft回答：

```
5 个 replica：

S1
S2
S3
S4
S5

怎样保证：

log[42] = X

不会在另一边变成：

log[42] = Y
```

ZooKeeper回答：

```
我已经有了一个可靠 replicated service。

现在：

Controller A
Controller B
Controller C

怎么安全决定：
谁是 leader？
```

所以：

```
Raft
= replication / consensus mechanism

ZooKeeper
= application-facing coordination service
```

ZooKeeper 本身使用的复制协议不是 Raft，而是 **Zab（ZooKeeper Atomic Broadcast）**。论文描述的是 leader-based atomic broadcast：所有更新经 Leader 排序，然后通过 Zab 广播，默认使用 majority quorum。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

### Paxos vs ZooKeeper

也是类似关系：

```
Paxos
    ↓
agreement primitive

Multi-Paxos
    ↓
ordered replicated log

ZooKeeper
    ↓
coordination abstraction
```

你可以认为：

```
Paxos / Raft / Zab
属于“发动机”

ZooKeeper
属于“整辆车提供给应用的接口”
```

但 Zab、Raft、Multi-Paxos 不是同一个算法，不能简单写：

```
Zab = Raft
```

只能说它们处在相似的 architecture layer。

---

### State Machine Replication vs ZooKeeper

State Machine Replication 是：

```
same ordered commands
        +
deterministic state machine
        ↓
same replicated state
```

ZooKeeper 内部正是维护 replicated state。

但它暴露给客户端的不是：

```
append arbitrary command()
```

而是：

```
create
delete
exists
getData
setData
getChildren
sync
```

然后应用基于它们组合更复杂协议。ZooKeeper 论文明确列出了这些 API，并使用 version number 支持 conditional update。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

### ZooKeeper vs 2PC

它们非常容易被放到一起，其实解决的问题不同。

```
ZooKeeper:
多个 process 怎样协调？

2PC:
一个 transaction 跨多个 resource manager
怎样 atomic commit？
```

例如：

```
ZooKeeper:
谁负责 shard-17？

2PC:
DB-A 和 DB-B 的两个修改
是一起 commit 还是一起 abort？
```

ZooKeeper 可以帮助实现某些 transaction coordination pattern，但它不是：

```
任意资源上的分布式 ACID transaction manager
```

Apache 的 ZooKeeper recipes 甚至给出了用 ZooKeeper 构造 2PC 的示例，这恰好说明 **2PC 是可以构建在 coordination primitive 之上的高级协议，而不是 ZooKeeper 自身的核心 abstraction**。[Apache ZooKeeper](https://zookeeper.apache.org/doc/r3.7.2/recipes.html?utm_source=chatgpt.com)

---

### ZooKeeper vs Spanner

Spanner 的目标大得多：

```
distributed database
+
sharding
+
replication
+
transactions
+
MVCC
+
global timestamps
+
strong consistency
```

ZooKeeper主要管理：

```
small coordination metadata
```

例如：

```
/current-leader
/config/version
/members/node-001
/election/candidate-00000017
```

而不是存：

```
10 TB customer table
```

论文明确说 znodes 不是为通用数据存储设计的，而主要存 coordination metadata。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 3：最重要的 7 个 Mental Models

---

## Concept 1：Coordination Kernel

#### 它解决的问题

分布式系统有无数 coordination protocol：

```
Leader Election
Lock
Membership
Barrier
Configuration
Queue
Rendezvous
```

一种设计方法是：

```
ZooKeeper Server:
    acquireLock()
    electLeader()
    joinGroup()
    barrier()
    ...
```

问题是服务端 primitive 会越来越多。

ZooKeeper选择另一条路：

```
非常少的基础 primitive

+
强 ordering semantics

=
client 可以构造复杂 coordination protocol
```

#### 一句话

> **ZooKeeper 是 distributed coordination 的“小内核”。**

类似：

```
CPU primitive:
CAS

可以构造：
mutex
semaphore
lock-free queue
...
```

ZooKeeper：

```
create
delete
version
ephemeral
sequential
watch

可以构造：
leader election
lock
membership
barrier
...
```

这是论文非常核心的设计决策。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

## Concept 2：Znode

ZooKeeper 的 namespace 看起来像 filesystem：

```
/
├── config
│   ├── database
│   └── feature-flags
│
├── workers
│   ├── worker-1
│   └── worker-2
│
└── election
    ├── candidate-000001
    └── candidate-000002
```

每个节点：

```
znode
```

里面可以存：

```
data
version
metadata
```

但是 mental model 不要变成：

> ZooKeeper 是分布式文件系统。

不是。

这里的 filesystem hierarchy 主要是 **namespace organization**。

论文中的 replicated database 整个 data tree 保存在内存中，同时通过日志和 snapshot 实现恢复；论文实现中单个 znode 默认上限为 1 MB。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

## Concept 3：Ephemeral Znode

这是 ZooKeeper 最漂亮的 abstraction 之一。

普通节点：

```
Persistent

create
  ↓
一直存在
  ↓
直到显式 delete
```

Ephemeral：

```
Client Session
      |
      +------ /members/A
                    ^
                    |
                 ephemeral
```

只要 session 存活：

```
/members/A exists
```

session expire：

```
/members/A automatically disappears
```

论文中 session 有 timeout，并且可以在 ensemble 中不同 ZooKeeper server 之间重新连接；只有 session 真正结束/过期后，其 ephemeral state 才清理。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

于是 membership 极其自然：

```
/members
   ├── controller-a   ephemeral
   ├── controller-b   ephemeral
   └── controller-c   ephemeral
```

想知道存活成员：

```
getChildren("/members")
```

---

## Concept 4：Sequential Znode

create 时可以告诉 ZooKeeper：

```
SEQUENTIAL
```

例如三个客户端：

```
A create /election/candidate-
B create /election/candidate-
C create /election/candidate-
```

ZooKeeper生成：

```
/election/candidate-0000000001
/election/candidate-0000000002
/election/candidate-0000000003
```

sequence number 给我们一个非常宝贵的东西：

> **全局有序的竞争顺序。**

因此可以构造：

```
Leader Election
Lock queue
Read/Write Lock
Queue
```

官方 recipe 的 Leader Election 同样使用 `EPHEMERAL|SEQUENTIAL`。[Apache ZooKeeper](https://zookeeper.apache.org/doc/r3.7.2/recipes.html?utm_source=chatgpt.com)

---

## Concept 5：Version + Conditional Update

假设：

```
/config

value   = v1
version = 7
```

A读取：

```
value=v1 version=7
```

B也读取：

```
value=v1 version=7
```

然后：

```
A:
setData("/config", v2, expectedVersion=7)

success

version = 8
```

随后 B：

```
setData("/config", v3, expectedVersion=7)

FAIL
```

因为：

```
actual version = 8
expected       = 7
```

这就是：

```
Compare-And-Swap
```

式的 optimistic concurrency control。

论文 API 中的 update 接收 expected version，不匹配时失败；`-1` 表示跳过版本检查。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

这和你在数据库里见过的：

```
UPDATE resource
SET ...
WHERE version = 42;
```

本质非常接近。

---

## Concept 6：Watch

你可能第一反应：

```
while true:
    getData("/config")
    sleep(1)
```

这是 polling。

如果：

```
10000 clients
```

每秒一次：

```
10000 read/s
```

很多是无意义的。

ZooKeeper提供：

```
getData("/config", watch=true)
```

现在 ZooKeeper 告诉客户端：

```
/config changed
```

客户端重新读取。

但是一定记住：

> **Watch 是“你的数据可能已经 stale”的 notification，不是 change log。**

假设：

```
/config = v1

watch

v1 -> v2
v2 -> v3
v3 -> v4
```

你不一定得到：

```
event(v2)
event(v3)
event(v4)
```

论文原始 watch 是 one-shot trigger，多个变化可能合并成一个“changed”通知；正确模式是：

````
receive notification
       ↓
re-read current state
       ↓
install new watch
``` :chatgpt-content-reference{index="10"}


这跟 Kubernetes Controller 的思路其实非常接近：

```text
event
  ≠
truth

event
  =
something may have changed

truth comes from:
re-read current state
````

这是非常重要的 distributed systems mental model。

---

## Concept 7：Session

ZooKeeper不是把每条 TCP connection 当身份。

Client拥有：

```
ZooKeeper Session
```

session 可以：

```
Server 1
   X connection lost

Client
   ↓

Server 2
```

只要还没 session timeout：

```
same session
ephemeral nodes remain
```

这比：

```
TCP disconnect → immediately declare client dead
```

正确得多。

因为 transient network failure 极其常见。

---

# Part 4：System Model

现在明确论文到底假设什么。

### Node model

ZooKeeper论文假设 server：

```
crash-recovery
```

也就是说：

```
Server crash
   ↓
later restart
```

不是 Byzantine：

```
不会假设 server 恶意撒谎
不会任意伪造 transaction
不会发送互相矛盾的消息
```

论文明确说 server may crash and later recover。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

### Network model

实际通信通过网络，允许：

```
delay
connection loss
partition
server unreachable
```

ZooKeeper内部实现使用 TCP，利用 TCP 的连接内有序传输简化 Zab 消息处理。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

但是：

```
TCP ordered delivery
```

绝不等于：

```
distributed system 不会丢请求
```

连接断开以后，你仍然有经典 ambiguity：

```
Client ---- create() ----> ZooKeeper
                           create succeeds
              X
          response lost
```

此时客户端不知道：

```
create成功了吗？
```

ZooKeeper官方 recipe 特别提醒 sequential ephemeral create 的这个 recoverable error，并建议加入 GUID 后通过 `getChildren()` 确认此前的创建是否成功。[Apache ZooKeeper](https://zookeeper.apache.org/doc/r3.7.2/recipes.html?utm_source=chatgpt.com)

---

## Timing model

Safety 的 reasoning 不依赖：

```
固定网络延迟上界
```

但 practical failure detection 和 liveness 使用 timeout。

因此工程上最合适的 mental model 是：

```
asynchronous-ish network for safety

+

eventual timing assumptions for liveness
```

也就是你熟悉的：

```
partial synchrony
```

网络如果无限期 partition：

```
没有 quorum
    ↓
progress impossible
```

但 safety 不应该因此被破坏。

---

## Storage model

ZooKeeper：

```
           memory
             |
             v
       full data tree

同时：

WAL / transaction log
             +
          snapshot
             ↓
      persistent disk
```

论文实现要求 update 在应用到 in-memory DB 前持久化相关日志，并周期性 snapshot。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

## Failure tolerance

默认 majority quorum：

```
N = 2f + 1
```

如果：

```
N = 3

f = 1
```

可以容忍：

```
1 server failure
```

如果：

```
N = 5

f = 2
```

可以容忍：

```
2 server failures
```

论文明确给出的关系就是：

````
2f + 1 servers
→ tolerate f failures
``` :chatgpt-content-reference{index="15"}


为什么？

5台：

```text
A B C D E
````

majority：

```
3
```

即使死2台：

```
A B C
```

还有3台，可以形成 quorum。

死3台：

```
A B
```

不能继续安全决定新的 write。

此时应该：

```
unavailable

而不是：

split brain
```

这是 Consensus 系统非常核心的选择：

> **宁愿失去 availability，也不能产生两个相互冲突的历史。**

---

# Part 5：ZooKeeper Architecture

整体架构：

```
                Client A
                   |
                   v
              ZK Server 1
                   |
                   | write
                   v
                Leader
              /    |    \
             /     |     \
            v      v      v
           S1      S2     S3
              Zab / quorum

          replicated database
```

每个 ZooKeeper server 都能接 client。

这是很重要的。

不是：

```
所有 client
    ↓
Leader
```

而是：

```
Client A -> follower 1
Client B -> follower 2
Client C -> leader
```

读：

```
local replica
```

写：

```
forward to leader
→ Zab
```

论文 architecture 正是：read 从 local database 直接处理，而修改状态的请求通过 agreement protocol。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 6：一次 Write 的 Happy Path

假设：

```
Client
 connected to
    |
    v
Follower F1
```

客户端：

```
setData("/config", "v2", version=7)
```

### Step 1

F1收到请求：

```
F1
 |
 +----> ZooKeeper Leader
```

### Step 2

Leader 做 validation：

```
expectedVersion == current/futureVersion ?
```

然后把 client operation 转换成一个确定的、可重放的：

```
transaction
```

例如概念上：

```
SetDataTxn(
    path="/config",
    data="v2",
    newVersion=8
)
```

论文特别强调：Leader 把 request 转成 **idempotent transaction**，使 recovery 时重复 delivery 也可以接受。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

### Step 3

Leader：

```
Zab broadcast
```

```
          Leader
         /      \
        v        v
       F1        F2
```

等 majority。

### Step 4

proposal committed。

所有 replica 最终按相同顺序 apply：

```
/config = v2
version = 8
```

### Step 5

Client 所连接的 server deliver 这个 update 后：

```
response success
```

论文说明 write 通过 Zab 广播，默认 majority quorum；接收 client request 的 server 在对应状态变化被 deliver 后回复 client。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 7：为什么 Read 不走 Consensus？

这里是 ZooKeeper 最值得学习的 performance trade-off。

如果所有 read 都：

```
Client
  ↓
Leader
  ↓
Quorum
  ↓
Response
```

那么：

```
read scalability 很差
```

但 ZooKeeper workload 往往：

```
read >> write
```

于是它选择：

```
read
  ↓
connected ZooKeeper server
  ↓
local memory
```

因此：

```
Client A → S1 read
Client B → S2 read
Client C → S3 read
```

可以并行。

代价是什么？

> **普通 read 不是 linearizable read。**

---

# Part 8：ZooKeeper 最容易搞错的 Consistency Model

这是整节 Lecture 最重要的地方之一。

ZooKeeper论文核心保证：

```
1. Linearizable writes

2. FIFO client order
```

论文原文将自己的 write property 称为 A-linearizability；所有修改 ZooKeeper state 的请求都有全局 ordering，而同一个 client 的请求按照发送顺序执行。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

但：

```
reads are local
```

所以：

> **ZooKeeper ≠ 所有 operations 都 linearizable。**

这是很多人第一次学 ZooKeeper 最大的误区。

---

### 一个具体例子

最初：

```
x = 0
```

S1、S2：

```
S1: x=0
S2: x=0
```

Client A：

```
write x=1
```

写已经 committed。

可能出现短暂状态：

```
S1: x=1
S2: x=0  ← lagging
```

Client B 此时连接 S2：

```
read x
```

可能得到：

```
0
```

即使：

```
write(x=1)
```

已经完成。

论文明确指出，local fast read 可能返回 stale value，即使较新的 update 已经 committed。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

## 那为什么这种 consistency 还能做 coordination？

这就是论文真正聪明的地方。

很多 coordination protocol 不要求：

```
every read globally linearizable
```

它们真正需要的是：

```
writes 有一个全局顺序
+
自己的 operations 保持顺序
+
变化时可以被通知
+
需要强读时有办法强制 catch up
```

也就是：

```
ordered writes
+
FIFO
+
watch
+
sync
```

这个组合足以构造大量强 coordination primitive。论文甚至指出，即使只有 writes 是 linearizable，这套 API 仍然足以构建他们所需的 coordination primitives。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf?utm_source=chatgpt.com)

---

# Part 9：sync() 是干什么的？

考虑：

```
A:
ZooKeeper write config=v2

A ---- other channel ----> B
        "config changed"

B:
ZooKeeper read config
```

B连接的 ZooKeeper follower 可能落后：

```
B sees v1
```

这就很奇怪：

```
A已经告诉B：
“配置更新了”

B再去ZooKeeper读：
“怎么还是旧配置？”
```

因此：

```
B:
sync()
getData()
```

`sync()` 会确保当前 server catch up 到调用 sync 时之前的 pending updates，然后 FIFO ordering 保证后面的 read 不跑到 sync 前面。论文就是为了这个场景引入 `sync`。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

所以：

```
普通 read
    =
fast but potentially stale

sync + read
    =
slow but fresher / ordered wrt prior writes
```

这是一种很经典的：

```
consistency ↔ scalability
```

trade-off。

---

# Part 10：FIFO Client Order 为什么如此重要？

假设 Leader 修改大量 config：

```
delete("/ready")

set("/A", A2)
set("/B", B2)
set("/C", C2)

create("/ready")
```

应用约定：

```
/ready exists
    ↓
configuration may be used
```

如果 ZooKeeper允许一个 client 的 operations reorder：

```
create("/ready")

跑到

set("/B")
```

前面，就可能：

```
observer sees ready
        ↓
reads partially updated config
```

但 FIFO client order 保证：

```
delete ready
     ↓
config updates
     ↓
create ready
```

顺序不会乱。

这就是一个极好的例子：

> ZooKeeper 不是靠“大事务 API”解决所有问题，而是用足够强的 ordering primitive 让 client protocol 自己建立 invariant。

论文专门用这个 configuration example 说明 FIFO ordering 和 watch ordering 的用途。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 11：Leader Election 一步一步来

现在来看最经典的 ZooKeeper recipe。

我们有：

```
/election
```

三个 Controller：

```
A
B
C
```

---

### Naive version

所有人尝试：

```
create("/leader", EPHEMERAL)
```

A成功：

```
/leader = A
```

B、C失败。

A session expire：

```
/leader automatically deleted
```

B、C重新竞争。

这已经能工作。

但是有一个性能问题：

```
10000 candidates

全部 watch /leader

/leader deleted

10000 clients wake up
10000 create()
```

这叫：

```
Herd Effect
```

---

## 改进：EPHEMERAL + SEQUENTIAL

A：

```
create(
  "/election/candidate-",
  EPHEMERAL | SEQUENTIAL
)
```

得到：

```
candidate-000001
```

B：

```
candidate-000002
```

C：

```
candidate-000003
```

于是：

```
/election

000001  ← leader
000002
000003
```

最小 sequence：

```
leader
```

---

### 但不要所有人 watch Leader

否则仍然 herd effect。

改成：

```
000001

   ↑ watch

000002

   ↑ watch

000003
```

准确来说：

```
candidate-2
watch candidate-1

candidate-3
watch candidate-2
```

A失败：

```
000001 disappears
```

只有 B 被唤醒。

B检查：

```
我是最小的吗？

yes
```

B成为 leader。

C什么都不做。

ZooKeeper官方 recipe 正是让每个 candidate 只 watch 自己的 immediate predecessor，以避免 herd effect。[Apache ZooKeeper](https://zookeeper.apache.org/doc/r3.7.2/recipes.html?utm_source=chatgpt.com)

---

# Part 12：Distributed Lock

其实 Leader Election 和 Lock 几乎是同一个结构。

假设：

```
/locks/database
```

A：

```
lock-000001
```

B：

```
lock-000002
```

C：

```
lock-000003
```

规则：

```
lowest sequence owns lock
```

因此：

```
A owns lock

B waits A
C waits B
```

A：

```
delete lock-000001
```

或者：

```
A session expires
```

那么：

```
lock-000001 disappears
```

B醒来：

```
B owns lock
```

论文给出的 lock recipe 就是：

````
create EPHEMERAL|SEQUENTIAL
getChildren
如果自己 sequence 最低 -> acquired
否则只 watch predecessor
``` :chatgpt-content-reference{index="25"}


---

# Part 13：这里隐藏着一个极重要问题——Distributed Lock ≠ Fencing

回到我们一开始的问题。

假设：

```text
A owns ZooKeeper lock
````

然后 A 和 ZooKeeper network partition。

A：

```
卡了 30 秒
```

ZooKeeper：

```
A session expires

delete A ephemeral node
```

于是 B acquire lock。

现在：

```
B owns lock
```

但 A突然恢复执行：

```
A:
"I still remember I owned the lock!"
```

如果 A直接调用外部 Cloud API：

```
A ----------> Storage

B ----------> Storage
```

ZooKeeper本身不能穿过网络，把 A 的 CPU 停下来。

这就是：

> **Distributed lock 的 ownership state，不等于对外部 resource 的 physical revocation。**

这正是为什么 robust distributed systems 经常需要：

```
Fencing Token
```

---

### 利用 sequence number 做 fencing token

假设：

```
A lock token = 41

B lock token = 42
```

Storage记住：

```
last_seen_token = 42
```

A恢复：

```
write(..., token=41)
```

Storage：

```
41 < 42

REJECT
```

B：

```
write(..., token=42)

ACCEPT
```

于是：

```
ZooKeeper
负责 ownership ordering

external resource
负责 fencing enforcement
```

这是工程实践里极其重要的边界：

```
Leader Election
       ≠
旧 Leader 自动失去所有副作用能力
```

---

# Part 14：Membership

这一点非常直观。

节点启动：

```
Worker A:
create("/workers/A", EPHEMERAL)
```

Worker B：

```
create("/workers/B", EPHEMERAL)
```

于是：

```
/workers
├── A
└── B
```

Coordinator：

```
getChildren("/workers", watch=true)
```

现在 A session expire：

```
/workers/A disappears
```

Coordinator收到：

```
membership changed
```

重新：

```
getChildren()
```

得到：

```
B
```

论文就利用 ephemeral znode 构造 group membership。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

但注意：

```
ZooKeeper认为A session expired
```

不是哲学意义上的：

```
A机器一定彻底死亡
```

它表示：

> **在 ZooKeeper 的 session/failure-detector 语义下，A 已经丧失这个 membership identity。**

这是非常重要的区别。

---

# Part 15：Failure Detection 到底怎么做？

ZooKeeper session timeout 就是一个 Failure Detector。

简化：

```
Client
   |
heartbeat / requests
   |
ZooKeeper ensemble
```

长时间没收到：

```
session timeout
```

ZooKeeper：

```
expire session
```

然后：

```
delete ephemeral nodes
```

论文描述的 client library 会在 session 空闲到一定程度发送 heartbeat，如果当前 server 无响应则尝试连接另一个 server；Leader 依据整个 ensemble 对该 session 的观测来判断 session timeout。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

这跟你学过的 Failure Detector 完全对应。

ZooKeeper不可能做到：

```
Perfect Failure Detector
```

网络 partition：

```
A alive

but

ZooKeeper cannot communicate with A
```

最终也可能 expire A。

所以这里的真正语义不是：

```
"A is dead"
```

而是：

```
"A no longer owns a valid ZooKeeper session"
```

这是更准确的说法。

---

# Part 16：一次 Failure Timeline

看看 Leader A：

```
time →

A:  owns lock
    |-----------------------------
    |
    | network partition from ZK
    |
ZK: |--------- waits timeout
                     |
                     X session expired
                     |
                     delete /lock/A
                              |
B:                            acquire lock
                              |
                              | work
A:                                  resumes
                                    |
                                    | stale work
```

ZooKeeper内部 invariant 没问题：

```
同时有效的 lock ownership
只有一个。
```

但 application-level invariant：

```
同时只有一个 process
能修改 external DB
```

不一定成立。

所以：

```
ZooKeeper lock

+
fencing / idempotency / generation

=
更完整的 correctness story
```

---

# Part 17：ZooKeeper Server 内部 State

可以建立这样一个简化 mental model：

```
ZooKeeper Server

persistent:
    transaction log
    snapshots

memory:
    znode tree
    sessions
    watches
    latest zxid
```

其中：

#### Znode tree

```
/
├── config
├── workers
└── locks
```

application state。

#### Transaction log

记录：

```
ordered state changes
```

用于 crash recovery。

#### Snapshot

避免 restart 时：

```
从 transaction #1
一直 replay 到 transaction #3,000,000,000
```

#### zxid

可以理解成：

```
ZooKeeper transaction position / ordering identifier
```

read response 会关联服务器最后看到的 zxid。客户端换服务器时，新 server 必须至少 catch up 到 client 已经看到过的 zxid，避免客户端 view 倒退。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

#### Session state

跟踪：

```
client session
timeout
ephemeral nodes
```

#### Watches

跟踪：

```
哪个 client
正在 watch 哪个 znode
```

论文实现中 watch notification 由客户端当前连接的 server 本地维护和触发。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 18：一个非常有意思的机制——Fuzzy Snapshot

这部分非常值得你这种做 Infra 的工程师关注。

最 naive snapshot：

```
stop all writes

copy entire state

resume
```

问题：

```
large tree
→ long pause
```

ZooKeeper选择：

```
不停机 snapshot
```

可能得到：

```
/foo = new
/goo = old
```

甚至这个组合：

```
在真实执行历史中从未同时存在过
```

听起来很危险。

但是 ZooKeeper transaction 被设计成：

```
idempotent
```

snapshot 之后从合适位置：

```
ordered replay transactions
```

就可以把 state 修正到正确结果。

论文给出了 `/foo`、`/goo` 的例子，并明确指出 fuzzy snapshot 本身可能不是任意时刻的合法完整状态，但按顺序重放 idempotent transactions 后可以恢复正确状态。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

这是一个非常漂亮的设计：

```
不要为了 snapshot 一致
暂停整个在线系统。

允许 snapshot fuzzy

+
让 replay mechanism
具备修复能力。
```

你以后看数据库 checkpoint、LSM snapshot、incremental snapshot 时会反复见到这种思想。

---

# Part 19：核心 Invariants

ZooKeeper最值得记住的 invariant 大概有这些。

### Invariant 1：Write 有唯一全局顺序

例如：

```
W1: x=1
W2: x=2
W3: x=3
```

不会：

```
Replica A:

W1 W2 W3

Replica B:

W2 W1 W3
```

Zab 对状态修改提供 ordered atomic broadcast。论文特别要求，新 Leader 在广播自己的更新前，必须先处理旧 Leader 已有的更新序列。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

否则 replica 会 diverge。

---

### Invariant 2：同一 Client 的操作保持 FIFO

Client：

```
delete ready
set A
set B
create ready
```

不会执行成：

```
create ready
set A
...
```

这让客户端能够利用操作顺序构造 protocol。

---

### Invariant 3：Version update 是原子的

两个：

```
setData(version=7)
```

不能同时成功。

否则：

```
lost update
```

就会发生。

---

### Invariant 4：一个 session expire 后，其 ephemeral state 必须消失

否则：

```
dead member
```

永远留在 membership：

```
ghost member
```

或者：

```
dead leader
```

永远持锁。

---

### Invariant 5：客户端已经观察过的历史不能在 reconnect 后倒退

如果客户端已经看到：

```
zxid=100
```

重新连接某个 lagging server：

```
zxid=90
```

不能立即让 client 从那里继续工作。

论文实现要求新 server 至少 catch up 到客户端已看到的 zxid 后再重建 session。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 20：Correctness

现在分 Safety / Liveness。

## Safety

最核心含义：

> **系统不会因为 crash、retry、leader change 等情况产生两个相互冲突的 committed write history。**

具体表现：

```
所有 committed writes
存在统一 order
```

由什么保证？

```
Zab
+
majority quorum
+
leader ordering
+
durable transaction log
```

---

## Distributed Lock Safety

假设：

```
lock-000001
lock-000002
lock-000003
```

定义：

```
minimum sequence owns lock
```

那么同一 ZooKeeper state 上：

```
minimum
```

只能有一个。

因此：

```
A owns
B waits
C waits
```

不会同时：

```
A owns
B owns
```

这是 coordination-state 层面的 mutual exclusion。

但再次强调：

```
ZooKeeper lock safety
≠
external resource fencing safety
```

两层 invariant 不一样。

---

## Liveness

ZooKeeper希望保证：

```
如果 majority servers
存活且可以互相通信

→ service eventually progresses
```

论文明确说，majority active and communicating 时服务可用；successful change 只要未来仍有 quorum 能恢复，就保持 durable。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

如果：

```
5 servers

partition:

A B        C D E
```

右侧：

```
3
→ quorum
→ can progress
```

左侧：

```
2
→ no quorum
→ cannot safely commit writes
```

因此：

```
Safety maintained
Availability lost on minority
```

---

## Safety ≠ Liveness

如果网络：

```
partition forever
```

正确系统完全可以：

```
never make progress
```

但只要它没有：

```
commit contradictory histories
```

Safety 仍成立。

所以：

```
Safety:
bad thing never happens

Liveness:
good thing eventually happens
```

ZooKeeper/Raft/Paxos 都必须用这个框架分析。

---

# Part 21：Failure Matrix

|Failure|ZooKeeper行为|Safety|Availability|关键机制|
|---|---|---|---|---|
|Follower crash|其他副本继续|保持|有 quorum 则继续|Replication|
|Leader crash|重新选 Leader|保持|短暂停顿|Zab leader recovery|
|单个 client crash|session 最终 expire|保持|其他 client 继续|Session + Ephemeral|
|Packet loss|retry/reconnect|保持|可能暂时降低|Session / protocol|
|Response loss|client可能不知道操作是否成功|保持|client需恢复|GUID/version/retry logic|
|Network partition|majority side继续|保持|minority不可写|Quorum|
|Delayed packet|protocol ordering处理|保持|latency增加|Zab/order|
|ZK server restart|log + snapshot recovery|保持|可恢复|WAL + Snapshot|
|Client暂停超过 timeout|session失效|ZK内部保持|client失去 ownership|Session expiration|
|Old leader恢复执行|ZK lock已无效|ZK内部保持|外部系统可能危险|需要 Fencing|
|Watch期间多次变化|可能只收到一次通知|保持|正常|re-read state|

其中两个尤其值得牢记：

```
successful server-side execution
+
lost RPC response
```

是一种 **uncertain outcome**。

以及：

```
session expired
+
old process still running
```

是一种 **stale actor problem**。

这两种错误在真实系统设计中非常常见。

---

# Part 22：Top 5 Misconceptions

### ❌ 1. ZooKeeper 所有 read 都是 Linearizable

错。

正确：

```
writes:
globally ordered / linearizable in paper's sense

ordinary reads:
local
potentially stale
```

需要时：

````
sync()
+
read
``` :chatgpt-content-reference{index="34"}


---

## ❌ 2. ZooKeeper 就是一个 Distributed Lock Service

不是。

论文甚至明确强调：

```text
ZooKeeper itself
不是专门的 lock server API

lock 是 client recipe
``` :chatgpt-content-reference{index="35"}


ZooKeeper更准确的定位：

```text
coordination substrate / kernel
````

---

### ❌ 3. Ephemeral node 消失 = process 一定死了

错。

可能只是：

```
long GC pause
network partition
process freeze
ZooKeeper unreachable
```

更精确的含义：

```
ZooKeeper session expired
```

---

### ❌ 4. Watch 是可靠 change-event stream

错。

不要想成：

```
Kafka events
```

而应该想成：

```
cache invalidation hint
```

它告诉你：

```
your cached state may be stale
```

正确做法：

```
watch fired
    ↓
read current state
    ↓
install watch again
```

论文原始 watch 是 one-shot，且多个改变可能合并。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

### ❌ 5. ZooKeeper lock 可以阻止旧 Leader 继续写数据库

错。

ZooKeeper可以说：

```
A no longer owns lock
```

但无法做到：

```
kill A's CPU
```

必须考虑：

```
Fencing Token
Generation
Epoch
Version
Idempotency
```

这条对做 Cloud Control Plane 尤其重要。

---

# Part 23：ZooKeeper 和 Raft 到底是什么关系？

你现在可以画成：

```
                         Application
                              |
                    coordination recipes
                              |
      +-----------------------+------------------+
      |                       |                  |
Leader Election             Lock            Membership
      |                       |                  |
      +-----------------------+------------------+
                              |
                         ZooKeeper API
                              |
              +---------------+---------------+
              |                               |
       local fast reads                ordered writes
                                              |
                                             Zab
                                              |
                                       replicated log
                                              |
                                         replicas
```

而 Raft：

```
Application
     |
 Replicated Service
     |
    Raft
     |
 replicated log
```

ZooKeeper的 lesson 是：

> **Consensus 自身通常不是应用真正想要的 abstraction。**

应用真正想要：

```
who is leader?
which workers exist?
what is config?
who owns this task?
has everyone reached barrier?
```

因此我们通常会：

```
Consensus
   ↓
Replicated Metadata Service
   ↓
Higher-level Coordination
```

你熟悉的：

```
Raft
 ↓
etcd
 ↓
Kubernetes
```

就是同样的 architecture pattern。

---

# Part 24：ZooKeeper vs etcd

这是最适合你建立直觉的对应关系之一。

```
ZooKeeper                   etcd

Zab                          Raft
 |                            |
replicated metadata          replicated KV
 |                            |
coordination                 coordination/state
```

两者都常被用于：

```
small critical metadata
configuration
membership-like information
leader election primitives
```

但 consistency/API 设计不同。

一个尤其重要的区别是：

```
ZooKeeper paper:
ordinary local reads may be stale

etcd:
API提供 linearizable read 与 serializable/stale-friendly read
```

所以 mental model 可以类比，但不能认为它们只是 API 不同的同一个东西。

---

# Part 25：和 Kubernetes 联系

你非常适合从这个角度理解 ZooKeeper。

假设三个 kube-controller-manager：

```
Controller A
Controller B
Controller C
```

你不会让三个同时执行所有 singleton control loops。

所以存在：

```
Leader Election
```

现代 Kubernetes 常通过：

```
Lease object
```

在 API Server / etcd 这一一致性基础设施之上完成。

抽象上：

```
               Raft
                 ↓
                etcd
                 ↓
           Kubernetes API
                 ↓
              Lease
                 ↓
        Controller Leader Election
```

对应 ZooKeeper：

```
                Zab
                 ↓
             ZooKeeper
                 ↓
 EPHEMERAL + SEQUENTIAL znode
                 ↓
           Leader Election
```

同一种系统设计思想：

> **不要让业务 Controller 自己实现 Consensus。**

---

# Part 26：和 Kubernetes Controller 的 Watch 联系

你平常最熟悉：

```
Informer receives event
        ↓
enqueue key
        ↓
Controller GET current state
        ↓
reconcile
```

它和 ZooKeeper watch 有一个非常好的共同 mental model：

```
notification
    ≠
authoritative state

notification
    =
“something changed;
please inspect current state”
```

所以正确 architecture 永远倾向：

```
Event
   ↓
Trigger
   ↓
Read source of truth
   ↓
Reconcile
```

而不是：

```
Event
   ↓
assume every intermediate event was delivered
```

当然 Kubernetes watch 的具体 semantics 和 ZooKeeper original one-shot watch 不一样，这里只是 mental model 类比。

---

# Part 27：和 Cloud Control Plane 联系

假设你的系统：

```
                  Global Control Plane
                           |
                     metadata state
                           |
               +-----------+----------+
               |           |          |
           Controller   Controller  Controller
```

ZooKeeper-style coordination 可以承担：

```
/regions/us-east-1/leader
/regions/us-east-1/controllers/*
/operations/op-123/owner
/config/network/version
```

例如：

```
Operation Worker
     |
     | create EPHEMERAL_SEQUENTIAL
     v
/operations/op123/claim-00042
```

最低 sequence worker owns operation。

但真正调用：

```
AWS API
GCP API
Azure API
```

时仍应考虑：

```
Idempotency Key
Generation
Operation ID
Desired State
CAS
Fencing
```

因为 coordination service 无法替你保证 external API 的 side effect。

这个边界在 Cloud Control Plane 里非常关键。

---

# Part 28：和 Terraform 的关系

Terraform典型思路：

```
Desired State
     ↓
Diff
     ↓
Apply
```

ZooKeeper解决的不是 Terraform dependency graph。

它解决的是：

```
多个 Terraform/Controller process
如果可能同时操作 shared resource

谁拥有 mutation authority？
```

一个典型 pattern：

```
Distributed Coordination
          ↓
      acquire ownership
          ↓
      read generation
          ↓
       execute work
          ↓
     idempotent effects
```

这就是为什么：

```
Distributed Lock
```

通常只是安全系统的一部分，而不是整个答案。

---

# Part 29：和 Kafka 的关系

ZooKeeper历史上最著名的用户之一就是 Kafka。

非常适合用来理解它为什么存在：

```
Kafka data plane:

partition logs
messages

不是放在 ZooKeeper
```

ZooKeeper承担的是历史上的：

```
cluster coordination
metadata
broker presence
controller election
```

所以：

```
ZooKeeper
≠
Kafka message storage

ZooKeeper
=
Kafka coordination metadata infrastructure
```

这正符合 ZooKeeper：

```
small important metadata
not bulk data
```

的设计定位。

---

# Part 30：和 Distributed Cache 联系

比如：

```
1000 cache servers
```

ZooKeeper不负责：

```
cache value itself
```

而可能负责：

```
which nodes are alive?
which configuration generation?
who owns shard 73?
```

因此：

```
Data Plane        Coordination Plane

Redis/Memcached   ZooKeeper
actual cache      metadata/membership
```

这是 Control Plane / Data Plane 分离的经典案例。

---

# Part 31：Paper Problem

对应论文是：

**ZooKeeper: Wait-free Coordination for Internet-scale Systems**, USENIX ATC 2010。[USENIX](https://www.usenix.org/conference/usenix-atc-10/zookeeper-wait-free-coordination-internet-scale-systems?utm_source=chatgpt.com)

论文的问题非常明确：

> 大规模 distributed application 反复需要 configuration、membership、leader election、locks 等 coordination mechanism；怎样提供一个足够通用、fault-tolerant，同时还能承受很大规模读取 workload 的基础服务？

---

## Previous Approach

可以有：

```
专门做 Lock Service
专门做 Leader Election Service
专门做 Membership Service
```

或者提供非常强的 blocking abstraction：

```
lock()
unlock()
```

ZooKeeper作者认为，这容易：

```
限制应用表达能力

以及

让 server core
依赖 slow/faulty clients
```

所以他们不把所有高层 coordination primitive 塞到 server side。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

## Key Insight

整篇论文最值得记的一句话：

> **提供简单、wait-free 的共享 metadata objects，加上足够强的 ordering 和 notification semantics，让 client 自己实现复杂 coordination primitive。**

其中组合是：

```
hierarchical znodes
+
conditional version updates
+
ephemeral nodes
+
sequential nodes
+
sessions
+
watches
+
FIFO client ordering
+
linearizable writes
```

---

## “Wait-Free” 到底是什么意思？

这里容易和你在并发课程里学的 formal wait-free 搞混。

ZooKeeper的核心思想是：

```
基础 znode operation
不需要等待其他 application client 主动协作
```

例如 server 不提供：

```
lock()  // block until holder unlocks
```

而是：

```
create(...)
exists(...)
watch(...)
```

然后：

```
客户端自己 wait
```

所以一个 slow client 不应该卡住 ZooKeeper server processing。

但千万不要理解成：

```
ZooKeeper 在任意 network partition 下
任何 operation 都保证完成
```

没有 quorum 时：

```
write cannot progress
```

论文的 wait-free motivation 与传统 shared-object wait-free 理论相关，但工程语境上你要重点理解：

````
non-blocking coordination kernel

vs

server-side blocking lock abstraction
``` :chatgpt-content-reference{index="39"}


---

# Part 32：Paper Design

架构：

```text
                  clients
            /       |       \
           v        v        v
         ZK1       ZK2      ZK3
                    |
                  Leader
                /   |   \
               /    |    \
              v     v     v
             ZK1   ZK2   ZK3
                 Zab

       replicated in-memory tree

                +
             WAL

                +
            snapshots
````

所有 server：

```
serve reads locally
```

writes：

```
Leader
 ↓
Zab
 ↓
quorum
```

这一个决定解释了论文几乎所有 performance characteristics。

---

# Part 33：Paper Evaluation

论文实验非常漂亮，因为它直接验证 design trade-off。

100% read workload：

```
3 servers   ≈ 87k ops/s
5 servers   ≈ 165k
7 servers   ≈ 257k
9 servers   ≈ 296k
13 servers  ≈ 460k
```

而 100% write：

```
3 servers   ≈ 21k ops/s
13 servers  ≈ 8k ops/s
```

原因：

```
read:
local memory
→ more replicas = more read capacity

write:
atomic broadcast
+
persistent logging
+
more replicas
→ more coordination overhead
```

论文正是以此说明 read-local design 对 read-heavy workload 的扩展性，同时也展示 replica 数量增加对 write broadcast throughput 的代价。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

所以你可以直接得到一个系统设计原则：

```
Replication

不天然等于：

more throughput
```

可能是：

```
read throughput ↑

write throughput ↓
```

取决于 consistency protocol。

---

# Part 34：Paper Failure Evaluation

论文还在 5-server ensemble 上主动 kill servers。

观察到：

```
Follower failure
→ 通常仍有 quorum
→ 服务继续

Leader failure
→ leader election
→ 短暂 interruption

loss of quorum
→ 无法继续正常 progress
```

当时实验中的 leader election 通常在 200 ms 内完成；这是该论文测试环境中的 measurement，不应理解为所有现实部署的固定 SLA。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf)

---

# Part 35：Paper Limitations

ZooKeeper没有试图解决：

```
bulk data storage
SQL
distributed query
arbitrary multi-resource ACID transaction
Byzantine faults
global geo-distributed DB
automatic external-resource fencing
```

它的优势正来自于 scope 很窄：

```
small coordination metadata
```

这种“少做一点，但是把这一点做到非常可靠”实际上是 ZooKeeper 最大的工程价值。

---

# Part 36：What Aged Well?

很多设计到今天仍然非常重要：

```
Consensus-backed metadata service

Session / Lease style liveness

Ephemeral membership

Versioned CAS

Watch-driven control plane

Leader Election on top of common substrate

Control Plane / Data Plane separation

Fencing generation / epoch thinking
```

尤其：

```
Consensus
    ↓
Metadata Store
    ↓
Controllers
```

这已经成为现代 infrastructure architecture 的标准 pattern。

---

# Part 37：哪些思想后来出现了不同实现？

今天很多系统不一定直接使用 ZooKeeper。

你会看到：

```
ZooKeeper
etcd
Consul
database leases
Raft-backed metadata services
custom control-plane stores
```

实现不同，但问题没有消失：

```
membership
ordering
ownership
configuration
leader election
failure recovery
```

还是那些问题。

所以 ZooKeeper 这节课并不是：

> 学一个老软件。

而是：

> 学 coordination service 这一类系统为什么长成这样。

---

# Part 38：它和 6.824 Lab 的关系

ZooKeeper通常不是让你直接实现一遍。

真正重要的关系是：

```
Raft Lab
    ↓
你正在实现 ZooKeeper 底下那类东西

ZooKeeper Lecture
    ↓
告诉你应用为什么需要它
```

在当前 6.5840 课程中，ZooKeeper lecture 紧邻 fault-tolerant KV lab；该 lab 要求把 KV service 建在 Raft-based replicated state machine 上，并要求 client-visible history linearizable。不同年份 6.824/6.5840 的 lab 编号有所变化。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

你在 Lab 里实现：

```
term
log[]
commitIndex
apply
snapshot
RPC retry
deduplication
```

这些属于：

```
“How do we build reliable replicated state?”
```

ZooKeeper则告诉你：

```
一旦有这个 state，
可以向上构造什么？
```

---

# Part 39：Problem → Solution Chain

把整节课压成你最想要的形式：

```
Distributed applications
需要 coordination
        ↓
每个应用自己实现 Consensus
太难
        ↓
建立 fault-tolerant
coordination service
        ↓
Naive:
在服务端直接提供 Lock /
Leader Election 等高级 primitive
        ↓
问题：
primitive 固定
blocking client 会影响 server
        ↓
提供小型 coordination kernel
        ↓
hierarchical znodes
        ↓
需要知道 process 是否活跃
        ↓
Session + Ephemeral
        ↓
需要多个 contender 有顺序
        ↓
Sequential znode
        ↓
需要防止 concurrent lost update
        ↓
Version + conditional update
        ↓
需要避免 polling
        ↓
Watch
        ↓
所有 write 必须有一致顺序
        ↓
Zab + Quorum
        ↓
如果所有 read 也走 consensus
read scalability 差
        ↓
local read
        ↓
出现 stale read
        ↓
FIFO ordering + watch
以及必要时 sync()
        ↓
可以构造
Leader Election / Lock /
Membership / Configuration
        ↓
但是旧 owner 仍可能产生 external side effect
        ↓
application 仍需
Fencing / Generation / Idempotency
```

这条链如果你真正理解了，ZooKeeper 就已经学会了 80%。

---

# Part 40：30 秒版本

如果面试官问：

> ZooKeeper 是什么？为什么需要它？

你可以回答：

> ZooKeeper 是一个 fault-tolerant distributed coordination service。它把 Consensus/Replication 这种复杂机制封装成一个小型 metadata service，提供 hierarchical znodes、sessions、ephemeral/sequential nodes、versioned conditional updates 和 watches。应用利用这些 primitive 构建 Leader Election、Membership、Distributed Lock、Configuration 等机制。ZooKeeper 的重要性能设计是 writes 通过 Zab 全局排序，而普通 reads 可以由 replica 本地处理，因此 reads 可以扩展，但可能 stale；需要更强 ordering 时可以使用 sync。它真正解决的是 distributed application 如何安全地协调，而不是存放大量业务数据。

---

# Part 41：3 分钟版本

更完整一点：

```
Raft/Paxos
解决 replica agreement。

但应用真正需要的是：
谁是 Leader？
谁还活着？
谁拥有 task？
配置是什么？
```

如果每个应用自己实现 Consensus，成本很高。

ZooKeeper因此提供：

```
small replicated coordination kernel
```

内部：

```
Zab
+
majority quorum
+
WAL
+
snapshot
```

外部：

```
znode
session
ephemeral
sequential
version
watch
```

例如 Leader Election：

```
A → candidate-001
B → candidate-002
C → candidate-003
```

节点是：

```
EPHEMERAL + SEQUENTIAL
```

最小 sequence 是 leader。

每个 candidate 只 watch 前驱：

```
003 → watch 002
002 → watch 001
```

这样 leader crash/session expiry 后：

```
001 disappears
→ only 002 wakes
```

避免 herd effect。

ZooKeeper consistency 很有意思：

```
writes globally ordered
client requests FIFO

but

ordinary read may be stale
```

这样 read 可以从每个 replica 本地读取，提升 read throughput。

如果需要读取至少包含此前变化：

```
sync + read
```

最后一个工程重点是：

```
ZooKeeper lock
不能自动阻止 stale process
访问 external resource
```

因此真正安全的系统通常还要：

```
fencing token / epoch / generation
```

---

# Part 42：深入版本

完整 mental model：

```
Problem
    ↓
distributed processes require coordination

Model
    ↓
crash-recovery servers
network partition
majority quorum
persistent log + snapshots

Core Abstraction
    ↓
hierarchical versioned znodes
sessions
ephemeral state
sequential ordering
watches

Replication
    ↓
leader-based Zab
ordered writes
majority commit

Read Optimization
    ↓
replica-local reads
possibly stale

Ordering
    ↓
linearizable/A-linearizable writes
FIFO per-client ordering
sync for stronger read ordering

Recipes
    ↓
Leader Election
Lock
Membership
Configuration
Barrier

Safety
    ↓
one global committed write order
CAS protects conditional state transition
ephemeral ownership removed after session expiry

Liveness
    ↓
majority reachable
→ eventual progress

Failure Handling
    ↓
server crash → replication
leader crash → new leader
client crash → session expiry
partition → quorum side continues
retry ambiguity → application recovery logic

Trade-offs
    ↓
fast scalable reads
but stale reads

small coordination metadata
but not general-purpose storage

lock ownership
but external resource still needs fencing
```

---

# Part 43：知识网络

最终把它挂到你的 Distributed Systems Mental Model：

```
                         Distributed Systems
                                |
             +------------------+-------------------+
             |                                      |
         Replication                            Coordination
             |                                      |
      Consensus / Ordering                           |
             |                                      |
       +-----+------+                                |
       |            |                                |
     Raft          Zab ------------------------ ZooKeeper
       |             |                                |
       |        Replicated State                      |
       |                                              |
State Machine Replication                     Coordination Kernel
                                                      |
                          +---------------------------+------------------+
                          |             |             |                 |
                     Leader Election  Lock        Membership      Configuration
                          |
                  Ephemeral + Sequential
                          |
                     Session Timeout
                          |
                    Failure Detector
                          |
                    stale leader risk
                          |
                      Fencing Token
```

如果只留下三条长期记忆，我建议是：

```
① Consensus 不是应用最终想要的 abstraction。
   ZooKeeper 把 consensus-backed state
   转化成 coordination primitives。

② ZooKeeper 的精髓不是“强一致 KV”：
   它故意选择
   globally ordered writes + fast local reads +
   FIFO + watch + sync，
   在 consistency 与 scalability 之间做设计。

③ Distributed Lock 只解决 ownership ordering。
   session expiry 后旧 process 仍可能活着；
   涉及 external side effect 时一定继续想到
   fencing / epoch / generation / idempotency。
```

这三条其实比记住 ZooKeeper 的全部 API 更重要。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/zookeeper.pdf?utm_source=chatgpt.com)

---

### 最后给你 5 道推理题

**Level 1：** A 成功创建 `/leader` 的 ephemeral node 后，与 ZooKeeper partition 了 20 秒，但仍然可以访问 MySQL。session timeout 是 10 秒。B 随后获得 Leader。为什么 ZooKeeper 本身没有违反 lock safety，但 MySQL 仍可能被两个“Leader”同时修改？

**Level 2：** A 已经成功完成 `setData(x=1)`；随后完全独立的 B 连接一个 lagging ZooKeeper follower 并立即 `getData(x)`。B 是否一定读到 1？如果不一定，怎样改变操作使 reasoning 更强？

**Level 3：** `/lock` 下存在：

```
lock-0007  A
lock-0008  B
lock-0009  C
```

为什么 C 应该 watch `0008` 而不是 `0007`？如果所有 10,000 个 contenders 都 watch `0007` 会发生什么？

**Level 4：** 如果去掉 ZooKeeper 的 **FIFO client ordering**，下面协议：

```
delete("/ready")
setData("/A", ...)
setData("/B", ...)
create("/ready")
```

可能出现什么违反 application invariant 的 execution？

**Level 5：** 如果让你给一个 Kubernetes-style multi-region control plane 设计 `region owner election`，请你区分四层：

```
Consensus layer
Coordination layer
Ownership token
External-cloud side-effect protection
```

分别需要什么机制？

这 5 道如果你能完整 reasoning 出来，ZooKeeper 这节课的核心基本就真正掌握了。