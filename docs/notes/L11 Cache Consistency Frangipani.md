# MIT 6.824 Lecture 11 — Frangipani：Cache Consistency

先给你整节课最重要的那句话：

> **Frangipani 真正解决的不是“怎么做一个分布式文件系统”，而是：多个 File Server 都直接访问同一份共享存储、又各自维护本地 cache 时，怎样让所有机器仍然像在访问“同一个正确的文件系统”？**

而整篇 paper 最漂亮的 insight 是：

> **把 Distributed Lock 同时当成 Cache Coherence 的 ownership token。**
> 
> 持有 lock → 你有资格相信/修改自己的 cache。  
> 失去 lock → 你必须 flush 或 invalidate cache。

理解这句话，Frangipani 大约就理解了 60%。

---

# Part 1：这节课到底在解决什么问题？

假设我们有两个 File Server，共享底层磁盘：

```
Client A                         Client B
   |                                |
   v                                v
File Server A                  File Server B
   |  cache A                       |  cache B
   |                                |
   +-------------+  +---------------+
                 |  |
                 v  v
              Shared Disk
                 Petal
```

A 和 B 都能看到同一块 disk。

乍一看，这不是很好吗？

不需要：

```
A replication → B
```

因为数据本来就在同一块共享存储上。

问题是：**为了性能，A 和 B 都必须 cache。**

比如文件：

```
/foo = "hello"
```

A 读一次：

```
Disk:    hello
A cache: hello
```

B 也读：

```
Disk:    hello
A cache: hello
B cache: hello
```

现在 A 修改：

```
/foo = "world"
```

为了性能，使用 write-back cache：

```
Disk:    hello
A cache: world   <- dirty
B cache: hello
```

此时 B：

```
read("/foo")
```

读自己的 cache：

```
hello
```

错了。

---

## naive solution 1：每次都读共享磁盘

不要 cache：

```
read()
  ↓
Petal
```

自然不会 stale。

但是这样 File Server 前面的 cache 基本失去意义，大量请求都会落到底层 storage。

---

## naive solution 2：写的时候立即写 disk

```
A write
  ↓
Petal
```

但 B 仍可能：

```
B cache = old value
```

所以：

> **write-through 并不能自动解决 cache coherence。**

问题不仅是：

```
dirty data 有没有写回 disk
```

还有：

```
其他机器什么时候知道
自己的 cache 已经失效？
```

---

## naive solution 3：A 修改时通知所有 File Server

理论上：

```
A:
write x

broadcast:
"invalidate x!"
```

于是你马上进入分布式系统经典问题：

```
B crash 呢？
消息丢了呢？
B 与 A partition 呢？
新加入的 C 呢？
invalidate 和 read 并发呢？
两个 writer 同时写呢？
```

所以 Frangipani 要解决的核心问题其实是：

```
Shared Storage
      +
Local Caches
      +
Concurrency
      +
Crash
      +
Network Partition
      ↓
怎样仍保持 coherent filesystem？
```

Frangipani 的答案就是：

```
Distributed Locks
      ↓
控制 cache ownership
      ↓
flush / invalidate
      ↓
Cache Coherence
```

Frangipani 的多个 server 运行在共享 Petal virtual disk 上，并通过 distributed lock service 协调；论文明确把 lock service 同时用于共享数据同步和跨 server buffer-cache coherence。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

# Part 2：它在 6.824 的知识地图里在哪里？

可以把前面的课串成这样：

```
RPC
 ↓
让机器互相调用

Concurrency
 ↓
多个请求会同时修改 state

Replication / Raft
 ↓
机器 crash 后 state 怎么保存

ZooKeeper
 ↓
分布式 coordination 怎么做

Distributed Transactions
 ↓
多个对象如何原子更新

                ↓

Frangipani
 ↓
多个 server 同时 cache
同一份 shared mutable state
时怎么保持 coherence
```

Frangipani 最值得学的地方是：

> **它展示了 Consensus 之外另一类极重要的分布式问题：Cache Coherence。**

---

## Frangipani vs Raft

Raft 解决：

```
多个 replica
怎样对操作顺序达成共识？
```

典型：

```
Client
   ↓
Leader
   ↓
replicated log
   ↓
Follower
```

所有副本通过一条 ordered log 收敛。

Frangipani 不是这样。

```
Server A ──┐
Server B ──┼──> same Petal virtual disk
Server C ──┘
```

它们不是：

```
对每一个 filesystem operation 做 Consensus
```

而是：

```
谁持有哪些 locks
        ↓
谁现在有权访问哪些 shared blocks
```

所以：

```
Raft
= replicated state coordination

Frangipani
= shared-state concurrency + cache coherence
```

---

## Frangipani vs State Machine Replication

SMR：

```
same ordered commands
        ↓
same deterministic state
```

Frangipani：

```
different servers
operate concurrently
on different parts of shared state
```

比如：

```
A owns lock(file1)
B owns lock(file2)
C owns lock(dir3)
```

三台机器可以并行执行。

这也是 Frangipani scalability 的来源之一。

---

## Frangipani vs ZooKeeper

ZooKeeper 是：

```
coordination service
```

提供：

```
ordered metadata
ephemeral nodes
watches
```

Frangipani 的 Lock Service 更专门：

```
multiple-reader / single-writer locks
+ leases
+ callbacks
```

Frangipani 真正重要的地方是：

> lock 不仅防止两个 writer，同时还驱动 cache invalidation。

---

## Frangipani vs Distributed Transaction / 2PC

Distributed transaction 问：

```
Resource A
Resource B
Resource C

commit 怎么做到 all-or-nothing？
```

Frangipani 的典型问题是：

```
inode
directory
allocation bitmap
file data
```

一次 filesystem operation 可能涉及多个结构。

Frangipani确实需要：

```
locks
+
logging
```

但它没有让多个独立 resource manager 跑 2PC。

所以：

```
2PC:
distributed atomic commit

Frangipani:
shared-storage concurrency
+ failure atomicity
+ cache coherence
```

---

# Part 3：六个核心 Mental Models

## Concept 1：Shared Disk ≠ Shared Filesystem

Petal 给 Frangipani：

```
一个大家都能读写的
fault-tolerant virtual disk
```

Petal 已经解决：

```
block 放在哪里
disk/server failure
storage replication
```

但是 Petal 不知道：

```
这个 block 是 inode
还是 directory
还是 file data

server cache 里有没有旧值

两个 filesystem operation 是否冲突
```

所以：

```
Petal
= storage abstraction

Frangipani
= filesystem semantics
```

这是非常重要的 layering。论文也把系统明确设计为这两层：Petal 提供 scalable/highly available virtual disk；Frangipani 在上面实现 shared coherent filesystem。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf?utm_source=chatgpt.com)

---

# Concept 2：Lock = Cache Ownership Token

这是这节课第一核心。

假设：

```
lock(file X)
```

不是简单意味着：

> “防止两个人同时进入 critical section。”

在 Frangipani 中它还意味着：

> **我的 cache 中关于 X 的副本目前是合法的。**

也就是说：

```
Lock State
   ↓
Cache State
```

被绑定起来。

---

### Read Lock

如果 A 持有：

```
R(X)
```

A：

```
可以读 X
可以 cache X
不能修改 X
```

多个 server 可以：

```
A: R(X)
B: R(X)
C: R(X)
```

因此 read sharing 很便宜。

---

### Write Lock

如果 A：

```
W(X)
```

则：

```
只有 A 能访问 mutable version
```

此时：

```
Disk X:  old

A cache:
X = new     DIRTY
```

是允许的。

关键 invariant：

> **cache 可以和 disk 不一致，当且仅当 server 持有对应的 write lock。**

论文直接规定：cached block 与 on-disk version 不同，只能发生在持有对应 write lock 时。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

这句话非常值得记住。

---

# Concept 3：Flush-before-transfer

现在 A：

```
W(X)

cache X = new
disk  X = old
```

B 要：

```
R(X)
```

Lock Server 不能直接：

```
grant B R(X)
```

否则：

```
B → disk → old
```

所以：

```
A                    Lock Service                  B

W(X)

                       B wants R(X)
                           |
      downgrade W→R <-----+
      |
flush X -> Petal
      |
ACK ---------------------->
                                              grant R(X)
                                                   |
                                              read Petal
                                                   |
                                                 new
```

注意顺序：

```
flush
 ↓
release/downgrade
 ↓
new owner
```

绝对不能：

```
release
 ↓
new owner
 ↓
flush
```

论文要求 write lock 在 release 或 downgrade 前将 dirty data 写回 Petal；read lock 被释放时则必须 invalidate cache。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

# Concept 4：Invalidate-before-release

反过来：

A 和 B 都：

```
R(X)
```

cache：

```
A: X=v1
B: X=v1
```

现在 C：

```
W(X)
```

C 必须成为唯一 writer。

于是：

```
Lock Service
   |
   +---- revoke A
   |
   +---- revoke B
```

A/B 不能只是：

```
release read lock
```

必须：

```
invalidate cache
        ↓
release lock
```

否则之后：

```
C write X=v2
A read cache
```

A 又读出 `v1`。

因此第二个核心 invariant：

> **没有 lock，就不能继续信任对应 cache。**

---

# Concept 5：Sticky Locks

如果每次：

```
read()
```

都：

```
acquire lock
read
release lock
```

Distributed Lock Service 会成为超级热点。

Frangipani 的 lock 是 **sticky**：

```
A acquire R(X)

read X
read X
read X
read X

仍然保留 R(X)
```

直到有人需要冲突 lock：

```
B asks W(X)
```

Lock Server 才 callback：

```
A:
please release R(X)
```

论文中的 lock client 默认会持有 lock，直到其他 client 请求冲突的 lock。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

因此 normal case：

```
Application
 ↓
local cache
 ↓
local cache
 ↓
local cache
```

不需要不断访问 Lock Server。

这和 CPU cache coherence 的 mental model 很像：

```
我目前拥有 cache line
        ↓
可以一直本地使用

别人请求 ownership
        ↓
invalidate / write-back
```

---

# Concept 6：Redo Log + Version Number

Locks 解决：

```
正常运行中的 concurrency
```

但是没有解决：

```
Server 在操作中间 crash
```

例如创建一个文件可能修改：

```
directory entry
inode
allocation bitmap
```

A：

```
update bitmap
update inode
---- CRASH ----
update directory
```

filesystem 可能被撕裂。

所以 Frangipani 每个 server 有自己的 **redo log**，放在共享 Petal 上；因此 server A crash 后，其他 server 可以读取 A 的 log 执行 recovery。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

# Part 4：System Model

这个部分非常关键，因为 Frangipani **不是 Byzantine / fully asynchronous system**。

## Node Model

Frangipani 假设：

```
trusted machines
single administrative domain
```

机器不会恶意撒谎。

因此：

```
Byzantine fault
❌ 不考虑
```

Frangipani File Server 可以 crash，并且可以由其他 server 根据其 persistent log recovery。

所以更接近：

```
crash-recovery
```

而不是纯 crash-stop。论文明确假设 Frangipani、Petal 和 lock servers 相互信任。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

## Storage Model

分成两层：

```
Frangipani cache
    |
    | volatile
    v
memory

Petal virtual disk
    |
    | persistent + replicated
    v
physical disks
```

每个 Frangipani server 的 redo log：

```
也存在 Petal
```

不是只存在本机 RAM。

---

## Network

会遇到：

```
network failure
network partition
delayed communication
```

Petal 能继续工作的条件包括：多数 Petal servers 可通信，并且相关数据至少存在一个可访问副本；distributed lock service 也需要多数 lock servers 可通信。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

## Timing Model

这里有一个非常值得你注意的点。

Frangipani 使用 **lease**。

例如论文实现中的 lease：

```
30 seconds
```

File Server 必须不断 renew。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

因此这不是完全：

```
pure asynchronous model
```

因为 lease：

```
依赖 clock / timeout
```

更适合按：

```
partially synchronous / practical timed system
```

来理解。

而且这里实际上藏着一个论文自己承认的 safety hazard，我们稍后专门讲。

---

# Part 5：完整算法——从 Happy Path 开始

先只考虑读。

# Case 1：A 第一次读 X

```
Client
  |
read(X)
  |
  v
Server A
  |
acquire R(X)
  |
  v
Lock Service
```

grant：

```
A holds R(X)
```

然后：

```
A -> Petal: read X

cache:
X = v1
```

之后再读：

```
read X
read X
read X
```

全部：

```
local cache
```

---

# Case 2：B 也读

B：

```
acquire R(X)
```

因为：

```
R + R compatible
```

现在：

```
A: R(X), cache=v1
B: R(X), cache=v1
```

没有问题。

---

# Case 3：C 想写

```
C asks W(X)
```

冲突：

```
A: R
B: R
C wants W
```

Lock Service：

```
revoke A
revoke B
```

A：

```
invalidate X
release R
```

B：

```
invalidate X
release R
```

现在：

```
C: W(X)
```

C：

```
cache X=v2
```

注意此刻可以：

```
disk  = v1
cache = v2
```

因为 C 是唯一 writer。

---

# Case 4：A 再读

A：

```
request R(X)
```

Lock Service 发现：

```
C holds W(X)
```

通知 C：

```
downgrade
```

C 必须：

```
flush v2 → Petal
```

于是：

```
Petal X = v2
```

然后：

```
C: W → R
```

此时 Lock Service 才：

```
grant A R(X)
```

A 从 Petal 得到：

```
v2
```

因此整个 chain 是：

```
Writer modifies cache
       ↓
Reader appears
       ↓
Lock conflict
       ↓
Writer flushes
       ↓
ownership transfer
       ↓
Reader fetches new value
```

这就是 Frangipani Cache Consistency Protocol。

---

# Part 6：把关键并发画成 timeline

```
time -------------------------------------------------------->

Server A:
W(X)
 |------ write X=2 ------|
 cache dirty
                         |
                         | flush X=2
                         v
Petal:
X=1 -------------------- X=2 ---------------------------->

Lock:
A owns W(X) ------------- downgrade ---- A:R, B:R

Server B:
                request R(X)
                       |
                       | waiting
                                  |
                                  +---- read X=2
```

现在问三个问题：

### 谁知道 X=2？

写完 local cache：

```
A 知道
Petal 不一定知道
B 不知道
```

### X=2 durable 吗？

不一定。

### B 什么时候允许读？

在：

```
A flush
+
lock downgrade
```

之后。

所以你可以看到：

> **Frangipani 不要求每个 write 都同步写 Petal；它只要求在 ownership 跨机器移动之前建立 coherence。**

这是性能关键。

---

# Part 7：Crash 后为什么需要 Log？

假设 A 创建：

```
/foo
```

涉及：

```
bitmap
inode
directory
```

A 已拿到相关 locks。

然后：

```
log(create foo)
        ↓
write bitmap
        ↓
write inode
        ↓
CRASH
        ↓
directory 没写
```

不能只说：

```
重新执行 syscall
```

因为 client 也可能死了。

所以恢复者读取：

```
A's redo log
```

重新完成缺失 metadata updates。

---

# 最微妙的问题：Recovery 会不会覆盖更新的数据？

这是 Frangipani 最值得思考的 correctness 问题之一，也是 MIT 经常拿来考的。

假设：

```
A modifies inode X
logA contains X=v1
```

后来：

```
B modifies X=v2
```

最后 A crash。

Recovery 看到：

```
logA says X=v1
```

如果直接 redo：

```
X=v2
 ↓
X=v1
```

你把 B 的新数据覆盖掉了。

灾难。

---

## Frangipani 怎么解决？

每个 metadata block 有：

```
version number
```

比如：

```
Disk block X:

version = 10
```

log：

```
update X
new_version = 11
```

recovery 时：

```
if disk.version < log.version:
    replay
else:
    skip
```

假设：

```
A log:
X version 11

B later writes:
X version 12
```

Recovery：

```
disk.version = 12
log.version  = 11

12 < 11 ?
false

skip
```

所以不会倒退。

论文就是通过 metadata block version 与 log record 的目标 version 对比，只在 block version 较旧时 replay。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

# 但为什么 B 能在 A 的 update 完成前拿到 lock？

答案是：

> 不能。

这是前一个 invariant 发挥作用：

```
dirty protected data
必须写入 Petal
       ↓
write lock 才能转移
```

所以：

```
A incomplete write
      ↓
A still owns W(X)
      ↓
B cannot update X
```

如果 A crash：

```
Recovery daemon
temporarily takes over
A's responsibilities
```

完成恢复后：

```
release lock
```

B 才能继续。

因此两个机制是组合起来保证 correctness：

```
Lock ownership
+
Redo log
+
Version number
```

而不是 version number 单独解决所有事情。论文指出 write lock 在 dirty state 被写入 Petal 前不会转移，因此任一 block 最多只有一个 log 含未完成的更新。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

# Part 8：Multi-object Operation

考虑：

```
rename("/a/x", "/b/x")
```

可能要同时修改：

```
directory A
directory B
inode X
```

需要：

```
L(A)
L(B)
L(X)
```

如果两台 server：

```
Server 1:
lock A
wait B

Server 2:
lock B
wait A
```

deadlock。

---

## Frangipani 的办法

它会：

```
Phase 1
确定需要哪些 locks

        ↓

按 inode address 排序

        ↓

Phase 2
按照全局顺序 acquire
```

例如：

```
A < B < X

永远：
lock A
lock B
lock X
```

这样避免 circular wait。

但 Phase 1 和 Phase 2 中间有一个问题：

```
你第一次看 filesystem
         ↓
release locks
         ↓
别人修改 filesystem
         ↓
你再 acquire locks
```

所以 acquire 完后：

> **必须重新验证之前观察到的 objects 有没有改变。**

如果改变：

```
release
retry
```

论文明确描述了这个“先发现需要的 locks，再排序 acquisition，最后 revalidate”的过程。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

这和数据库中的：

```
optimistic validation
```

有一点味道，但不要把它和完整 OCC 等同起来。

---

# Part 9：最重要的 Invariants

如果你准备考试，我建议记住下面五条，而不是背实现细节。

### Invariant 1

```
Dirty cached block
        ⇒
server holds write lock
```

否则：

```
两个 server 都可能认为自己能写
```

---

### Invariant 2

```
Write lock ownership changes
        ⇒
old owner's dirty data already reached Petal
```

否则新 owner 会基于旧数据工作。

---

### Invariant 3

```
Read lock release
        ⇒
invalidate cached copy first
```

否则之后可能读取 stale cache。

---

### Invariant 4

```
Recovery never overwrites
a newer completed update
```

通过：

```
metadata version number
```

实现。

---

### Invariant 5

```
one crashed server log
        ↓
one active recovery daemon
```

否则：

```
Recovery A
Recovery B
```

也会互相竞争。

Lock Service 会给 recovery daemon 对该 log 的 exclusive ownership。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

---

# Part 10：Safety 为什么成立？

现在把机制串起来。

## Safety 1：不能同时存在两个 writer

来自：

```
multiple-reader
single-writer lock
```

所以：

```
W(X) + W(X)
```

不允许。

---

## Safety 2：reader 不会读到旧 cache

要成为 writer：

```
所有 reader
必须先放弃 R lock
```

而：

```
release R
⇒ invalidate cache
```

所以 writer 修改之后：

```
旧 reader cache 已经不存在
```

---

## Safety 3：新 reader 会看到 previous writer 的 update

因为：

```
Writer W(X)
     ↓
reader asks R(X)
     ↓
writer flushes
     ↓
downgrade
     ↓
reader granted lock
```

所以 cache ownership transfer 本身构成：

```
happens-before boundary
```

直觉上：

```
previous writes
    happens-before
new owner's reads
```

---

# Cache Coherence 和 Linearizability 是不是一回事？

不是完全一回事。

Frangipani 的 coherence protocol 是一种 **机制**：

```
flush
invalidate
ownership
```

Linearizability 是一个 **外部 observable specification**：

```
每个 operation
看起来像在某一个瞬间执行
并遵守 real-time order
```

Frangipani paper 主要描述：

> 对不同机器提供 coherent、接近 local Unix filesystem 的可见性语义。

它不是把整个 POSIX interface 写成一个 formal Linearizability proof。

所以你可以建立直觉：

```
locks + cache coherence
帮助实现强一致的 shared filesystem semantics
```

但不要直接背：

```
Frangipani cache coherence == Linearizability
```

---

# Part 11：Liveness

Safety 是：

> 不要产生错误结果。

Liveness 是：

> 最终还能不能继续跑？

Frangipani：

```
Petal majority alive
+
Lock Service majority alive
+
network eventually usable
```

系统剩余 server 可以继续工作。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

但 partitioned-away 的 Frangipani server 会停止提供该 filesystem 的访问，而不是选择：

```
继续写，等以后 merge
```

所以这是一个非常典型的：

```
partition
   ↓
preserve consistency
   ↓
sacrifice availability
```

设计。

---

# Part 12：Leases，以及整篇 Paper 最现代的一个教训

现在进入特别值得你理解的地方。

假设：

```
A holds W(X)
```

然后 A 与 Lock Service partition：

```
A --------X-------- Lock Service
```

Lock Service：

```
30s lease expired
```

认为：

```
A dead
```

然后把 W(X) 给 B。

但是 A 实际没 crash。

如果 A 还继续写 Petal：

```
A stale writer
       ↓
Petal
       ↑
B new writer
```

系统坏掉。

---

## Frangipani 怎么处理？

正常情况下，A lease 失效后：

```
discard locks
discard cache
stop servicing requests
```

如果失联时有 dirty cache，它甚至进入 error 状态，后续访问报错，直到 filesystem 被 unmount/remount。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

但是还有一个微妙 race：

```
A:
check lease valid

lease = still valid
       |
       | send write
       v

========= network delay =========

lease expires

B receives lock
writes

========= old packet arrives =========

A's old Petal write arrives
```

论文明确承认：

> Petal 当时并不会验证每个 write 所带的 lease。

实现只用了一个较大的时间 margin（文中为 15 秒），作者明确说无法绝对排除这个 hazard。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

这非常重要。

---

# 现代答案：Fencing Token

今天设计这种系统，我们通常会说：

```
lease alone
≠
enough
```

需要：

```
fencing token
```

例如：

```
A gets lock:
token = 100

A partition

B gets lock:
token = 101
```

Storage 记录：

```
largest_token_seen = 101
```

现在 A delayed write：

```
WRITE X
token=100
```

Storage：

```
100 < 101

REJECT
```

B：

```
WRITE X
token=101

ACCEPT
```

所以真正强的 invariant 是：

> **旧 owner 即使还活着，也必须失去修改 shared resource 的能力。**

这就是 fencing。

有趣的是，Frangipani 论文自己就提出了一种接近这个方向的改进：让 Petal 验证 lease expiration，或者让每个 Petal write 携带 lease identifier，使失效 owner 的写入能被 storage 拒绝。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

从今天的 distributed systems 视角看，这段特别有价值。

---

# Part 13：Failure Matrix

|Failure|发生什么|Safety|Availability|Mechanism|
|---|---|---|---|---|
|Frangipani server crash|cache 丢失|metadata 可恢复|短暂下降|per-server redo log|
|Crash after log write|redo 未完成 update|保持|recovery 后恢复|log replay|
|Crash after metadata 已写|log 可能仍存在|保持|recovery|block version 防重复/旧 replay|
|Read-lock holder crash|lock 最终失效|依赖 lease|等 lease|lease|
|Write-lock holder crash|可能有 pending metadata|recovery 完成|短暂阻塞|log + recovery|
|Network partition from Lock Service|lease 无法 renew|选择停止服务|partitioned server 不可用|lease expiry|
|Network partition from Petal|无法读写 disk|不继续冒险|server 不可用|fail closed|
|Duplicate recovery|可能竞争|防止|—|recovery log exclusive lock|
|delayed old writer|可能危险|原实现存在 timing hazard|—|理想方案是 fencing|

---

# Part 14：Crash 后用户数据一定不会丢吗？

**不一定。**

这是非常容易误解的地方。

Frangipani 的 redo log 主要记录：

```
metadata
```

不是所有：

```
file user data
```

因此：

```
write()
return
```

不意味着：

```
data durable
```

这和经典 Unix filesystem 语义类似。

需要更强 durability：

```
fsync()
```

论文明确指出 user file data 并不写入这个 metadata redo log，因此 crash 后并不承诺完整的高层应用一致性；用户可以通过 `fsync` 获得更好的 durability semantics。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

因此再次区分：

```
Cache coherence
≠
Durability
```

你可以：

```
所有活着的 server
都不会读 stale cache
```

但：

```
power failure 之后
某个最近 write 仍可能消失
```

不矛盾。

---

# Part 15：为什么整个 File 用一个 Lock？

论文中典型粒度是：

```
one file
    ↓
one lock

inode
+
file contents
```

同一个 lock 一起保护。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

为什么不：

```
inode lock
data block locks
extent locks
```

因为复杂度。

工程 workload 假设：

```
不同机器频繁同时写同一个文件
```

比较少。

于是：

```
coarse-grained locking
```

优点：

```
lock 数量少
protocol 简单
common case 快
```

代价：

```
false contention
```

比如：

```
A 写文件 offset 0
B 写文件 offset 1GB
```

理论上不冲突。

Frangipani：

```
same file lock
```

仍然互斥。

这是经典：

```
lock granularity trade-off

coarse
  ↓
简单
低 overhead
低 concurrency

fine
  ↓
复杂
高 overhead
高 concurrency
```

---

# Part 16：Paper Problem / Key Insight / Evaluation

## Paper Problem

作者希望得到：

```
一个 filesystem namespace

多个 interchangeable file servers

共享 scalable storage

能横向加 server

还能保持 coherent Unix-like view
```

而不想搞：

```
一个 central filesystem server
```

成为吞吐瓶颈。

---

## Previous Approach 的痛点

传统：

```
Client
  ↓
Central File Server
  ↓
Disk
```

一致性简单：

```
只有一个 owner
```

但：

```
CPU bottleneck
network bottleneck
failover complexity
```

如果简单变成：

```
many file servers
     ↓
shared disks
```

马上遇到：

```
cache consistency
concurrent metadata modifications
recovery
```

---

# Key Insight

整篇 Frangipani 可以压缩成：

```
Petal
解决 scalable replicated storage

        +

Distributed Locks
解决 ownership + cache coherence

        +

Per-server Redo Logs
解决 crash recovery
```

三者组合：

```
        File System API
              |
      +-------+-------+
      | Frangipani FS |
      +-------+-------+
              |
       Lock Service
              |
       +------+------+
       |    Petal    |
       +-------------+
              |
        replicated disks
```

---

# Evaluation

论文的实验基本验证了两个方向。

对于写共享较少的开发类 workload，增加 Frangipani server 时 latency 增幅较小；论文报告 Modified Andrew Benchmark 从 1 台增加到 6 台时平均 latency 仅增加约 8%。Uncached read 的 aggregate throughput 也随着 server 数量较好地扩展。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

另一方面：

> **真正的敌人是 write sharing。**

整个 file 用 coarse-grained lock，一旦：

```
Reader
Writer
Reader
Writer
```

争抢同一个 file：

```
lock revoke
flush
invalidate
reload
```

不断发生，性能迅速下降。论文甚至观察到 read-ahead 在高 contention 情况下会浪费大量 I/O，因为刚 prefetch 完的数据很快又因 lock revocation 被 invalidate。[Stanford Center for Spatial Studies](https://www.scs.stanford.edu/nyu/03sp/sched/frangipani.pdf)

这说明一个很普遍的 distributed systems 原则：

> **一个 protocol 能不能 scale，不能只看 node 数量；还要看 workload 的 sharing pattern。**

---

# Part 17：Top 5 Misconceptions

## ❌ 1. Shared disk 天然意味着 cache consistent

错误。

```
shared persistent state
```

不等于：

```
shared volatile cache state
```

---

## ❌ 2. Distributed Lock 只是 mutex

在 Frangipani 中不是。

它还有第二层语义：

```
lock ownership
=
cache validity / write ownership
```

这是整节课核心。

---

## ❌ 3. Writer write-back 后就解决一致性了

不够。

Reader 的旧：

```
cached copy
```

还必须 invalidated。

所以必须同时有：

```
flush writer
+
invalidate old readers
```

---

## ❌ 4. Lease expiration 可以证明进程已经死了

不能。

只能说明：

```
我现在无法确认它仍然拥有 lease
```

进程完全可能还活着。

这就是：

```
stale owner
```

问题。

---

## ❌ 5. Lease 可以代替 fencing

不能。

如果旧 owner：

```
lease expired
```

却仍然能访问真正的 resource：

```
storage/database/device
```

那么它仍可能造成破坏。

所以：

```
coordination layer says you're dead
```

不够。

还需要：

```
resource layer refuses stale owner
```

---

# Part 18：和你熟悉的基础设施联系

这里有几个非常好的类比。

## Kubernetes Leader Election

假设：

```
Controller A
holds Lease

Controller B
waits
```

A 网络卡住。

Lease 到期：

```
B becomes leader
```

但是：

> A 是否立刻“物理死亡”？

不是。

A 可能仍然在运行。

如果 A 和 B 都能直接：

```
modify external resource
```

比如：

```
cloud DB
load balancer
volume
```

你就有：

```
stale leader
```

问题。

这和 Frangipani 的：

```
expired lock holder
still writes Petal
```

几乎是同一类问题。

---

## Terraform State Lock

Terraform：

```
state lock
```

主要避免：

```
Terraform A
Terraform B

同时改 state
```

但假设 A：

```
lost lock
```

A 已经发出去的：

```
CreateLoadBalancer()
```

不会因为 state lock 自动取消。

所以：

```
coordination ownership
```

和：

```
downstream side-effect fencing
```

仍然是两回事。

---

## etcd / Kubernetes resourceVersion

你可以把现代 fencing 直觉理解为：

```
generation / epoch / term
```

例如：

```
owner epoch=10
owner epoch=11
```

后端拒绝：

```
epoch=10
```

这和你学 Raft 时的：

```
term
```

也有同样的“旧时代请求不能覆盖新时代”的味道。

不过：

> Raft term、Kubernetes `resourceVersion`、fencing token 的具体语义并不相同。

这里类比的是：

```
monotonic generation
```

这种设计思想。

---

# Part 19：和旧版 6.824 Lab 的关系

Frangipani 曾经和经典 6.824 File Server Lab 非常直接相关。

旧版 Lab 要求：

```
多个 ccfs server
     ↓
shared block server
     ↓
local block cache
```

然后让学生加入：

```
locks
+
cache consistency
```

核心 invariant 几乎就是：

```
block 只有在持有
保护它的 lock 时
才能留在本地 cache
```

旧版 lab 文档也明确要求：release lock 时 flush/invalidate，从而既维持 cache consistency，又让无 contention 的访问停留在 local cache。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2004/labs/fs-lab-2.html?utm_source=chatgpt.com)

所以如果你自己实现一个简化版，我建议重点思考的不是代码，而是三个 invariant：

```
Who owns block X?

When may cache[X] be trusted?

What must happen before ownership moves?
```

只要这三个问题始终回答清楚，大部分 bug 都容易定位。

---

# Part 20：Problem → Solution Chain

这是今天最应该保存进脑中的链：

```
多个 File Server
访问同一 shared disk
        ↓

为了性能，各自 cache
        ↓

Problem:
cache 可能 stale
        ↓

Naive:
每次都访问 disk
        ↓

Failure:
性能差
        ↓

Mechanism:
multiple-reader /
single-writer lock
        ↓

Problem:
writer 的 dirty cache
还没写 disk
        ↓

Mechanism:
flush-before-lock-transfer
        ↓

Problem:
reader 仍保留旧 cache
        ↓

Mechanism:
invalidate-before-release
        ↓

Problem:
每次访问 lock service 太慢
        ↓

Mechanism:
sticky locks
        ↓

Problem:
lock holder crash
        ↓

Mechanism:
lease
        ↓

Problem:
filesystem update 做一半 crash
        ↓

Mechanism:
per-server redo log
        ↓

Problem:
old log replay 覆盖 newer update
        ↓

Mechanism:
metadata version number
        ↓

Problem:
lease expired 的旧 owner
可能仍然写 storage
        ↓

Frangipani 原实现:
timing margin

现代更完整答案:
fencing token
```

这条 chain 比记十页 paper 更重要。

---

# 30 秒版本

如果面试官问 Frangipani：

> Frangipani 是一个建立在 Petal shared distributed virtual disk 上的 cluster filesystem。多个 Frangipani servers 各自 cache 文件系统数据，因此核心问题是 cache coherence。它利用 sticky multiple-reader/single-writer distributed locks 作为 cache ownership token：read lock 被回收前 invalidate cache，write lock 被转移前必须把 dirty data flush 到 Petal。每个 server 还有 persistent redo log，并使用 metadata block version numbers 实现 crash recovery、防止旧日志覆盖更新数据。它非常好地展示了 locks、cache coherence、recovery 和 leases 如何组合。

---

# 3 分钟版本

可以记成：

```
                  Frangipani
                      |
          +-----------+-----------+
          |                       |
       Runtime                  Failure
       correctness              recovery
          |                       |
   Distributed Lock          Per-server log
          |                       |
 +--------+-------+               |
 |                |               |
R lock           W lock        Redo
 |                |               |
cache           dirty cache    Version
 |                |             number
invalidate       flush             |
before release  before transfer   avoid stale replay
```

核心不是：

```
distributed locking
```

本身，而是：

```
Lock State
    ↓
Cache Validity
```

之间的 coupling。

---

# 深入版本

完整 mental model：

```
Problem
    |
    | Multiple FS servers
    | + shared storage
    | + private caches
    v
Stale cache / concurrent modification
    |
    v
Distributed MR/SW Locks
    |
    +---- R lock → cache may be trusted
    |
    +---- W lock → cache may be dirty
    |
    v
Ownership Transfer
    |
    +---- R release → invalidate
    |
    +---- W release → flush
    |
    v
Cache Coherence
    |
    v
Crash Problem
    |
    v
Per-server Redo Log
    |
    v
Recovery
    |
    v
Old Log Problem
    |
    v
Metadata Version Number
    |
    v
Failure Detection
    |
    v
Lease
    |
    v
Stale Owner Problem
    |
    v
Fencing
```

---

# 最终知识网络

把今天这课挂到你的 Distributed Systems 知识树上：

```
                         Distributed Systems
                                  |
             +--------------------+-------------------+
             |                                        |
         Replication                            Shared State
             |                                        |
     Consensus / Raft                        Concurrency Control
             |                                        |
             |                              +---------+---------+
             |                              |                   |
           SMR                           Locks           Cache Coherence
                                                               |
                                                         Frangipani
                                                               |
                                            +------------------+---------------+
                                            |                                  |
                                     Cache Ownership                     Crash Recovery
                                            |                                  |
                                  R/W Distributed Lock                  Redo Log
                                            |                                  |
                                    flush / invalidate                  Version Number
                                            |
                                          Lease
                                            |
                                     stale-owner problem
                                            |
                                         Fencing
```

如果要把 **Frangipani 最深的一层 mental model** 压缩成一句：

> **它不是在“同步缓存内容”，而是在同步“谁有资格相信和修改缓存”；一旦 ownership 发生变化，就利用 flush 和 invalidation 把新的 ownership boundary 变成 coherence boundary。**

这也是为什么它值得出现在 6.824：它让你看到，除了 **Consensus 决定“大家同意什么”** 之外，分布式系统里还有一个同样重要的问题：

> **当多个节点各自持有 shared state 的局部副本时，谁现在拥有 authoritative copy，以及 ownership 转移时必须建立什么 invariant？**

这套思维以后会反复出现在 **distributed cache、database buffer pool、cluster filesystem、leader lease、storage fencing、controller failover** 里。