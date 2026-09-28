可以。下面把 **MIT 6.824 / 6.5840 Lecture 16 — Cache Consistency: Memcached at Facebook** 一次性完整串起来。

这节课对应 Facebook 2013 年 NSDI 论文 **Scaling Memcache at Facebook**。MIT 2026 的 6.5840 仍然把它作为 Lecture 16，紧接在 Optimistic Concurrency Control / verification 之后，用它讨论一个很有代表性的工程问题：**当 cache 不是 authoritative storage，却大到足以成为系统生死攸关的一层时，consistency、fault tolerance 和 scalability 应该怎么设计？** [MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

---

## 0. 先把整节课压成一句话

> **Facebook Memcache 的核心不是“怎样缓存数据”，而是怎样让一个允许丢数据、允许短暂 stale、没有 server-to-server coordination 的 cache，在数据库之前承受海量流量，同时防止 cache miss、并发和故障反过来摧毁数据库。**

这是整篇论文最重要的 mental model。

你学 Raft 时，思考的是：

```
怎样确保所有 replica 对“真相”达成一致？
```

今天的问题恰恰相反：

```
真相已经在 Database。

Cache 并不是真相。

那么：
我能不能故意放松 consistency，
换取极高性能和 availability，
又不至于让系统失控？
```

这就是这堂课真正有意思的地方。

---

# Part 1：这节课到底解决什么问题？

### 1.1 没有 cache 时

想象 Facebook 页面一次需要读取：

```
User
Posts
Friends
Comments
Likes
Photos
Permissions
...
```

最简单：

```
Web Server
     |
     | SELECT ...
     v
   MySQL
```

Facebook 当年的 workload 是明显 read-heavy，而且一个页面请求可能触发大量数据读取；论文报告某个流行页面平均会从 Memcache 获取 **521 个 item，95th percentile 达 1,740 个**。数据库当然不能直接承担所有这样的查询。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

所以加 cache：

```
                  +----------+
                  | Memcache |
                  +----------+
                       |
Web Server ------------+
     |
     | cache miss
     v
  Database
```

现在绝大多数读取不会到数据库。

问题看似解决了。

---

### 1.2 Cache-Aside

Facebook 使用的是典型的 **demand-filled look-aside cache**。

读：

```
GET cache[k]

hit:
    return cache[k]

miss:
    v = DB.read(k)
    SET cache[k] = v
    return v
```

写：

```
DB.write(k, newValue)
DELETE cache[k]
```

这里有一个非常重要的决定：

> **write 时不是更新 cache，而是删除 cache。**

因为 DB 才是 authoritative source，而 `delete` 是 idempotent；后续真正需要数据的 reader 再从 DB refill cache。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

## 1.3 第一个真正的 distributed systems bug：Stale Set

初始：

```
DB     x = 100
Cache  empty
```

Client A：

```
GET cache[x]
→ MISS

DB.read(x)
→ 100
```

但它读完之后卡了一会儿。

此时 Client B：

```
DB.write(x, 200)
DELETE cache[x]
```

然后 Client A 恢复：

```
SET cache[x] = 100
```

时间线：

```
time ------------------------------------------------------>

A: GET(x)
      MISS

A:       DB.read(x)
         → 100
              |
              | delayed
              |
B:                DB.write(x=200)
B:                     DELETE cache[x]
                            |
A:                              SET cache[x]=100
```

最终：

```
Database = 200
Cache    = 100   ❌
```

更糟糕的是：

```
之后可能没有任何 write
```

所以：

```
GET x
GET x
GET x
GET x
```

可能一直从 cache 返回 `100`。

这就是论文说的：

> **stale set**

论文的 lease 机制正是为了处理这个 race。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

## 1.4 第二个问题：Thundering Herd

假设：

```
key = celebrity:post:123
```

每秒：

```
100,000 GET
```

cache 中原来有：

```
post = P1
```

writer 更新 DB：

```
DB = P2
DELETE cache
```

瞬间：

```
              cache miss
           /      |       \
          /       |        \
       Web1     Web2 .... Web10000
        |         |           |
        +---------+-----------+
                  |
                  v
                MySQL
```

所有 Web Server 同时 miss。

于是：

> **缓存本来是保护数据库的，但一个 cache miss 可以瞬间把 cache 后面的数据库打死。**

这叫：

```
Thundering Herd
Cache Stampede
```

论文里的 lease 同时解决：

```
stale set
+
thundering herd
```

实际测量中，对一组特别容易出现 herd 的 key，lease 将数据库 peak query rate 从约 **17K/s 降到 1.3K/s**。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

因此：

```
Cache correctness
不是只关心：

“读到的值新不新？”

还关心：

“cache failure 会不会变成 database failure？”
```

这是今天第一个极重要的 mental shift。

---

# Part 2：在整个 6.824 里的位置

可以把你已经学过的东西放成：

```
                       Distributed Systems
                              |
       +----------------------+----------------------+
       |                                             |
Authoritative State                              Acceleration
       |                                             |
 Replication                                  Distributed Cache
       |                                             |
Consensus / Raft                                Memcache
       |                                             |
State Machine Replication                 Cache Consistency
       |                                      /    |     \
Linearizability                           stale  load   failure
       |
Distributed Transaction
       |
      2PC
       |
    Spanner
```

最关键的边界是：

```
Raft / Paxos
────────────────────────

我要确保：

replica A
replica B
replica C

最终认同同一个 authoritative history。


Memcache
────────────────────────

Database 已经是 authoritative。

Cache 可以：

evict
crash
lose data
temporarily stale

但不能让这种弱语义把整个 backend 拖垮。
```

所以：

#### Raft vs Memcache

Raft：

```
复制真相
```

Memcache：

```
复制真相的 disposable derivative
```

---

#### Linearizability vs Memcache

Linearizability：

```
Write(x=2) 完成
        ↓
之后的 Read(x)
        ↓
必须看见 x=2 或更新值
```

Facebook Memcache：

```
不提供这样的整体保证。
```

论文直接描述它为：

> **best-effort eventual consistency**

系统明确允许在一些情况下读取 transient stale data，以换取性能与 availability。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这是你理解整节课必须接受的前提：

> **这不是一个“弱版 Raft”。它从目标上就不是在实现 Linearizability。**

---

#### Distributed Transaction vs Memcache

你可能想：

```
BEGIN

UPDATE DB
UPDATE Cache

COMMIT
```

如果 DB 和 cache 可以参与一个 atomic transaction：

```
DB 200
Cache 200
```

理论上很多 race 会消失。

但是这样 cache 就变成 transaction participant：

```
DB
 |
2PC Coordinator
 |
Cache
```

cache 的：

```
低 latency
简单性
independent failure
随时 eviction
```

都会受到影响。

Facebook 的设计路线正好相反：

> **不要让 Cache 成为 authoritative transaction participant。**

因此 consistency 通过：

```
invalidate
lease
retry
replay
temporary staleness
```

来工程化管理。

---

# Part 3：7 个核心 Mental Models

---

### Concept 1：Memcached vs Memcache

论文特意区分：

```
memcached
=
单台机器上的 daemon / implementation

memcache
=
把大量 memcached + client + router + config
组合出来的 distributed system
```

原始 memcached 本质上只是一台机器上的 in-memory hash table，而且服务器之间没有 coordination。Facebook 把 routing、aggregation、configuration 等能力组合在客户端和周边系统中，构造出了 distributed cache。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这和你做 Kubernetes 很像：

```
一个 Pod
≠
一个 Platform
```

真正复杂的是外围：

```
routing
failure handling
load balancing
consistency
configuration
monitoring
```

---

## Concept 2：Demand-Filled Look-Aside Cache

核心：

```
                GET
Client ----------------------> Cache
                                 |
                         HIT ----+---- return
                                 |
                                MISS
                                 |
                                 v
                              Database
                                 |
                                 v
                              SET Cache
```

Cache：

```
不是 source of truth
```

因此：

```
cache miss
≠
data doesn't exist
```

只是：

```
“我这里目前没有。”
```

这是和 database KV store 很大的区别。

---

## Concept 3：Invalidation，而不是 Update Cache

写：

```
DB.update()
    ↓
cache.delete()
```

而不是：

```
DB.update()
    ↓
cache.set(newValue)
```

为什么？

考虑两个 writer：

```
W1:
DB = 1

W2:
DB = 2
```

数据库执行顺序：

```
W1 DB.write(1)
W2 DB.write(2)
```

但网络可能：

```
W2 cache.set(2)
W1 cache.set(1)
```

最终：

```
DB    = 2
Cache = 1
```

MIT 2024 的考试就专门考了这个 race。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q24-2-sol.pdf?utm_source=chatgpt.com)

而 delete：

```
DELETE x
DELETE x
```

无论谁先谁后：

```
x 不在 cache
```

所以它具有非常有价值的：

```
idempotency
```

这就是为什么 distributed system 非常喜欢：

```
DELETE
PUT desired state
UPSERT
SetDesiredState
```

这种可重试操作。

---

## Concept 4：Lease

Lease 是这一 Lecture 最值得吃透的机制。

直觉：

> **Cache miss 不意味着任何 client 都有资格把查询结果 SET 回来。**

而是：

```
GET x

MISS + lease token T
```

Cache 相当于告诉 client：

> 你现在有资格去 DB 查，然后把值填回来。

token 是与 key 绑定的 64-bit token。client refill 时：

```
SET x = value, token=T
```

Memcached 检查：

```
T 还有效吗？
```

如果期间有：

```
DELETE x
```

则：

```
T invalidated
```

因此 delayed reader：

```
Reader:
GET x
→ miss + T1

DB.read
→ 100

                Writer:
                DB = 200
                DELETE x

                DELETE invalidates T1

Reader:
SET x=100,T1

Memcache:
T1 invalid
→ reject
```

于是 stale set 被挡住。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这与 fencing token 的 mental model 非常接近：

```
“你曾经有权限”
≠
“你现在仍有权限”
```

但要注意：Facebook lease 不是一个完整 distributed lock。

它主要是在做：

```
conditional cache fill
```

---

## Concept 5：Lease 同时做 Request Coalescing

假设 cache miss 后：

```
Reader 1
Reader 2
Reader 3
...
Reader 10000
```

普通系统：

```
10000 DB queries
```

Lease：

```
Reader 1:
MISS + lease

Reader 2:
MISS + "someone is filling"

Reader 3:
MISS + "someone is filling"
...
```

Facebook 默认对同一个 key 大约 **10 秒只发一个 token**；这期间其他请求收到通知，短暂等待后重试。通常持有 lease 的 client 几毫秒就已经完成 refill。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

你可以把它理解成：

```
Go singleflight
```

例如：

```
v, err, _ := group.Do(key, func() (any, error) {
    return db.Load(key)
})
```

mental model 几乎一样：

```
1000 个相同 expensive operations
              ↓
           合并成 1 个
```

只不过 Facebook 将 arbitration 部分放到了 memcached。

---

## Concept 6：Gutter

如果一台 Memcache server 挂了：

naive 做法：

```
所有 GET
   ↓
cache server unavailable
   ↓
DB
```

这可能直接杀死数据库。

另一个 naive solution：

```
重新 consistent hash
```

把 key 分给剩余机器。

但 hot key 可能非常不均匀。论文举例说，一个 key 可能占某台服务器约 **20% 请求量**；将这些请求重新 hash 给其他本来就有负载的 memcached，可能造成 cascading failure。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

Facebook 的方案：

```
normal Memcache
      |
    failure
      |
      v
    Gutter
      |
    miss
      |
      v
      DB
```

Gutter：

```
一小组备用 cache
≈ cluster 中 memcached 数量的 1%
```

如果 Gutter miss：

```
DB load
→ SET Gutter
```

后续请求：

```
Gutter hit
```

所以：

> **Gutter 的首要任务不是保证 consistency，而是建立一个 failure shock absorber。**

论文报告，这一机制把 client-visible failure rate 降低了约 99%，并且在 Memcached server 完全故障时，Gutter hit rate 很快可以达到相当可观的水平。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这跟 Platform Engineering 中：

```
bulkhead
load shedding
circuit breaker
fallback cache
```

是一类思想。

---

## Concept 7：Invalidation Pipeline

一个 cluster 还能：

```
Writer
 |
DB
 |
Cache delete
```

但是一个 region 内可能：

```
                Storage Cluster
                     MySQL
                       |
      +----------------+----------------+
      |                |                |
Frontend A         Frontend B       Frontend C
Memcache           Memcache         Memcache
```

写发生后：

```
A cache
B cache
C cache
```

都可能存在旧数据。

所以需要广播：

```
DELETE k
```

Facebook 并不是完全依赖 Web Server 广播。

数据库 transaction 中记录需要 invalidation 的 cache key；transaction commit 后，数据库旁边的 **mcsqueal** 从 committed SQL / log 中抽取 invalidation，再向各 frontend cluster 广播。因为 invalidation 与数据库可靠日志关联，丢失或错误路由的 invalidation 可以 replay。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这是一个非常重要的思想：

```
Database Commit Log
        |
        v
Cache Invalidation Stream
```

你应该马上想到今天的：

```
CDC
Outbox
Debezium
Kafka
Change Streams
```

它们解决的核心问题都非常类似：

> **不要让 authoritative state change 与 derived-state notification 完全依赖两个互不相关的 best-effort RPC。**

---

# Part 4：System Model / Assumptions

这篇论文不像 Raft paper 那样给出严格 formal model。因此需要区分：

```
论文明确设计
vs
我们为分析而抽象出来的 system model
```

---

### Node Model

Memcached：

```
volatile
in-memory
can fail
can restart empty
can evict arbitrary cache item
```

所以：

```
Memcache data ≠ durable state
```

数据库：

```
authoritative
durable
```

论文中的 multi-region 架构则有：

```
master DB region
+
read-only replica DB regions
```

通过 MySQL replication 更新 replica。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

不是 Byzantine system。

主要关心：

```
crash
unresponsive node
packet loss
delay
misrouting
overload
network failure
```

---

## Network Model

实际上非常接近：

```
asynchronous network
```

因为不能假设：

```
bounded delay
```

例如：

```
DELETE
```

可能：

```
delayed
failed
buffered
retried
replayed
```

特别是 cross-region。

所以：

```
timeout
≠
proof server failed
```

跟你学 Failure Detector 时完全一致。

---

## GET 的网络选择

论文当年的具体实现中：

```
GET
→ 常使用 UDP

SET / DELETE
→ TCP via mcrouter
```

GET 出错时：

```
packet loss
out-of-order
late packet

→ treat as cache miss
```

而不是努力恢复 UDP RPC。论文报告 peak load 下约 0.25% 的 GET 被 client discard，并且 UDP GET 在其生产实验中降低了约 20% latency。SET/DELETE 因为代表 cache state change，因此通过 TCP 处理。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

注意：

> 这是 **2013 Facebook 的具体工程实现**，不是“现代 cache 必须用 UDP”的通用结论。

---

## Timing Model

不是 synchronous protocol。

也没有类似：

```
partial synchrony + majority
```

这样的 Consensus liveness assumption。

这里的设计思想更多是：

```
如果 delay 太大：
    tolerate stale
    retry
    fallback
    buffer
    reroute
```

---

## Consistency Model

这一点必须记住：

```
NOT Linearizable
NOT Serializable
NOT strict cache coherence
```

论文自己把整体设计描述为：

```
best-effort eventual consistency
```

并明确将：

```
读取 transient stale data 的概率
```

当作一个可以和 responsiveness 一样调节的工程参数。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

# Part 5：从 Simple Case 一步步长成 Facebook Memcache

下面按照：

```
simple
→ failure
→ mechanism
```

走一遍。

---

## Stage 1：Single Cache

```
Web
 |
Memcache
 |
DB
```

Happy path：

```
GET x
↓
cache hit
↓
return
```

Miss：

```
GET x
↓
MISS
↓
DB.read
↓
SET cache
↓
return
```

---

## Failure 1：stale set

```
Reader: GET miss
Reader: DB.read old

Writer: DB.update new
Writer: DELETE cache

Reader: SET old
```

加入：

```
Lease
```

---

## Stage 2：Lease

```
GET x
   |
MISS + lease T
   |
DB.read
   |
SET x,value,T
   |
cache verifies T
```

Invariant：

> **如果 GET 与 SET 之间出现了该 key 的 invalidation，那么旧 lease 必须失效。**

因此：

```
GET
        DELETE
SET
```

无法重新插入 deletion 之前读取的旧数据。

---

## Failure 2：Thundering Herd

```
DELETE hot-key

10000 readers
     |
10000 cache misses
     |
10000 DB reads
```

扩展 lease：

```
一次只让极少 client 获得 refill permission
```

于是：

```
one loader
many waiters
```

---

## Failure 3：All-to-All Fanout

一个 Web Request 可能：

```
GET 500 keys
```

keys 分散在：

```
M1
M2
M3
...
M100
```

如果串行：

```
M1 → wait
M2 → wait
M3 → wait
...
```

latency 爆炸。

因此：

```
parallel requests
+
batching
```

Facebook 根据数据 dependency 构造 DAG，尽量并行请求独立数据，并批量获取多个 key。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

## Failure 4：太多并行请求导致 Incast

你可能想：

```
那一次全发出去不就行了？
```

假设：

```
100 Memcache servers
          |
          | simultaneously reply
          v
      one Web Server
```

于是：

```
network queue overflow
packet loss
latency spike
```

这叫：

```
incast congestion
```

所以 client 又增加：

```
sliding window
```

例如：

```
最多同时发 100 个

response 到一个
→ 再发一个
```

窗口太小：

```
没有充分 parallelism
→ slow
```

窗口太大：

```
incast
→ packet loss
→ cache miss
→ DB load
```

因此存在一个 sweet spot。论文甚至用 Little's Law 分析这一现象。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

# Part 6：这里的 Little's Law

论文出现：

\[ L = \lambda W \]

符号：

```
L
=
系统里平均同时存在的 request 数量

λ
=
request arrival rate

W
=
每个 request 平均待在系统中的时间
```

例如：

```
λ = 1000 req/s

每个请求平均：
W = 0.1s
```

那么：

\[ L = 1000 \times 0.1 = 100 \]

平均大约有：

```
100 requests
```

同时存在。

如果因为 cache fetch 慢：

```
W = 0.5s
```

则：

\[ L = 1000 \times 0.5 = 500 \]

于是：

```
queue
memory
threads
scheduler pressure
```

全部增加。

所以：

> **Cache latency 不只是 page latency 问题，它还会增加整个 Web Server 中的 concurrency pressure。**

这就是为什么 Facebook 连：

```
request scheduling
UDP
batching
sliding window
```

都纳入 Memcache system design。

---

# Part 7：Memcache Pool

假设一个 pool 同时缓存：

```
A: 访问频繁、miss 很便宜

B: 访问不频繁、miss 极其昂贵

C: high churn

D: low churn
```

如果全部共享一个 LRU / memory pool：

```
high-churn data
```

可能不断把仍然有价值的：

```
low-churn expensive-to-miss data
```

挤出去。

于是 Facebook 把 key 根据 workload 特性分到不同：

```
Memcache Pools
```

例如：

```
wildcard pool
expensive-miss pool
application-specific pool
regional pool
replicated pool
```

论文强调，不同 workload 的 memory footprint、access pattern、churn 和 QoS 会相互产生 negative interference，因此 pool 是一种 workload isolation。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

你可以把它理解成：

```
cache QoS / cache tenancy isolation
```

和 Kubernetes：

```
Node Pool
ResourceQuota
PriorityClass
```

思想上相似：

> 不要让完全不同 workload 在一个共享资源池里互相破坏。

---

# Part 8：什么时候 Sharding，什么时候 Replication？

这个例子很有意思。

假设：

```
100 keys
```

每个 request：

```
都需要这 100 keys
```

一台 server：

```
500K req/s
```

现在要：

```
1M req/s
```

#### 方案 A：Sharding

```
Server A: 50 keys
Server B: 50 keys
```

每一个用户请求：

```
都必须请求 A
+
都必须请求 B
```

于是：

```
A = 1M req/s
B = 1M req/s
```

实际上 server request-rate 没降。

---

#### 方案 B：Replication

```
A: all 100 keys
B: all 100 keys
```

一半 clients：

```
→ A
```

另一半：

```
→ B
```

于是：

```
A 500K
B 500K
```

所以论文指出，在：

```
data set 小
每次查询很多 key
request rate 超高
```

这种 workload 下：

> **Replication 比进一步 sharding 更有效。**

代价则是：

```
所有 replica 都必须收到 invalidation
```

否则 consistency 更差。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这非常值得记住：

> **Sharding 解决 capacity，不一定解决 request fanout。**

---

# Part 9：Failure Handling — 为什么需要 Gutter

正常：

```
           key hash
              |
              v
          Memcache A
```

A 挂掉。

方案一：

```
DB fallback
```

假设 A 原来承担：

```
200K GET/s
```

现在：

```
200K DB query/s
```

数据库可能挂。

---

方案二：

```
consistent hash to B
```

但：

```
B 本来已经 70% load
```

而 A 里有一个 celebrity hot key。

现在：

```
B overloaded
↓
B fails
↓
redistribute again
↓
C overloaded
```

形成：

```
cascading failure
```

Facebook 于是使用专门的：

```
Gutter
```

```
             Normal
           Memcache A
               X
               |
               v
             Gutter
               |
              MISS
               |
               v
               DB
```

其目标不是：

```
perfect cache hit rate
```

而是：

```
在故障期间挡住 backend traffic spike
```

这是一个非常典型的：

> **failure-domain isolation**

---

## 一个反直觉点：为什么 Gutter 不接收正常 invalidation？

Paper 说 Gutter entry：

```
short-lived
```

因此他们接受：

```
slightly stale
```

来避免大量 normal invalidation traffic。

MIT 近年的 paper question 也专门问：

> 为什么 writer 不应该像普通 Memcache 那样给 Gutter 发 delete？[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/questions.html?lec=4&q=q-linear&utm_source=chatgpt.com)

Mental model 是：

```
Gutter 是 emergency shock absorber
```

如果所有 normal traffic 都必须维护：

```
perfect Gutter coherence
```

Gutter 就不再是便宜简单的 failure path，而且故障发生时它自己也更容易成为瓶颈。

它靠：

```
短 TTL
```

限制 stale 的生命周期。

---

# Part 10：从一个 Cluster 到一个 Region

现在拓扑变成：

```
                         Region
                           |
              +------------+------------+
              |                         |
        Storage Cluster            Frontend
           MySQL                     Clusters
                              +---------+---------+
                              |         |         |
                             C1        C2        C3
                              |         |         |
                           Memcache  Memcache  Memcache
```

同一个 DB value：

```
x
```

可能在：

```
C1 cache
C2 cache
C3 cache
```

存在三个副本。

于是：

```
DB.write(x)
```

必须使：

```
C1 DELETE x
C2 DELETE x
C3 DELETE x
```

发生。

Facebook 把 invalidation 信息与已提交数据库操作关联，由 mcsqueal 获取并向 frontend cluster 广播；mcrouter 再进行 batching / routing。论文报告这种 batching 将每 packet 中 deletes 的中位数提升了约 18 倍。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

架构：

```
              MySQL
                |
         transaction log
                |
                v
             McSqueal
                |
        batched invalidations
                |
     +----------+----------+
     |          |          |
  mcrouter   mcrouter   mcrouter
     |          |          |
 Memcache    Memcache   Memcache
 Cluster A   Cluster B  Cluster C
```

---

## 为什么不能单纯让 Web Server 广播？

因为：

```
Web Server
DB UPDATE success

then

send DELETE A
send DELETE B
send DELETE C
```

如果 Web Server 在中途 crash：

```
A deleted
B deleted
C still stale
```

而且如果：

```
network misconfiguration
routing bug
```

也不容易 replay。

数据库 log 则提供一个更加可靠的 anchor：

```
DB commit
   |
durable log
   |
invalidation derivation
   |
retry/replay
```

注意这仍然不是 distributed transaction。

但比：

```
random best-effort RPC
```

可靠得多。

---

# Part 11：Regional Pool

多个 frontend cluster：

```
Cluster 1 cache copy
Cluster 2 cache copy
Cluster 3 cache copy
Cluster 4 cache copy
```

对大而冷的数据：

```
4 copies
```

很浪费内存。

所以某些 key 改成：

```
              Regional Cache Pool
                 /   |   \
               C1   C2   C3
```

整个 Region 一份。

trade-off：

```
Cluster-local cache

+ lower latency
+ less cross-cluster bandwidth
+ failure isolation

- replication memory


Regional pool

+ less memory

- cross-cluster traffic
- extra latency
- larger shared failure domain
```

论文明确说选择依据包括：

```
access rate
data-set size
unique-user count
bandwidth cost
```

而不是一句：

```
“Replication 总是好”
```

。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

# Part 12：Cold Cluster Warmup

这是一个非常经典的 operational problem。

你新启动：

```
Cluster C
```

cache：

```
EMPTY
```

如果直接承接正常 traffic：

```
every request
    ↓
cache miss
    ↓
database
```

这叫：

```
cold cache
```

甚至可能把 DB 打挂。

Facebook：

```
Cold Cluster
     |
     | miss
     v
Warm Cluster
     |
     v
Database only if needed
```

论文报告 cold-cluster warmup 可以把恢复到完整容量所需时间从 **几天缩短到几小时**。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

### 但这里又出现一个 consistency race

假设：

```
Cold cache = empty

Warm cache = x=100
DB         = x=100
```

现在：

```
Writer:
DB = 200

DELETE cold x
DELETE warm x
```

问题是 invalidation 有 delay。

可能：

```
DB      = 200

Cold:
MISS

Warm:
still has 100
```

Cold：

```
GET warm
→ 100

SET cold=100
```

之后 warm 收到了 delete：

```
Warm empty
```

但 Cold：

```
still 100
```

于是 stale 被复制了。

---

## Facebook 的办法：delete hold-off

Cold cluster 上执行：

```
DELETE x
+
hold-off ≈ 2 seconds
```

这段时间：

```
ADD x
```

会失败。

流程：

```
Cold miss
   |
GET Warm
   |
value=100
   |
ADD Cold
   |
Rejected due to hold-off
   |
说明刚出现过 invalidation
   |
直接去 Database
```

默认 hold-off 是 2 秒。论文也非常坦率：

> 如果 invalidation 延迟超过这个窗口，理论上仍可能 stale。

但他们认为 rare consistency issue 的成本小于 warmup 的 operational benefit。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这是整篇论文最能体现 Facebook 哲学的一段：

```
不是证明：
“永远正确”

而是：

把 failure probability
控制到业务可接受范围
```

---

# Part 13：跨 Region 后，问题真正变难

现在：

```
                     Master Region
                         |
                    Master DB
                         |
              MySQL replication
                 /            \
                v              v
           Region B         Region C
           Replica DB       Replica DB
```

read：

```
就近 Memcache
↓ miss
就近 Replica DB
```

优点：

```
low latency
```

但出现：

```
replication lag
```

。

论文明确指出，跨 Region consistency 的主要挑战就是：

> **replica database may lag behind master database。** [USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

## Failure：Invalidation 比 DB Replication 更快

假设：

```
Master DB:
x 100 → 200
```

Region B：

```
Replica DB = 100
Cache      = 100
```

如果 master web server 立即向 Region B：

```
DELETE cache[x]
```

Region B：

```
Cache MISS
```

但 MySQL replication 还没赶上：

```
Replica DB = 100
```

于是：

```
DB.read = 100
SET cache=100
```

你反而主动制造 stale cache。

所以：

> **Invalidation 的正确时机必须和 replica progress 有关系。**

这就是为什么把 invalidation 与 database replication path 关联非常重要。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

这是非常深的一点：

```
DELETE sooner
```

并不永远意味着：

```
more consistent
```

如果 underlying source 仍 stale：

```
earlier DELETE
→ earlier refill
→ refill stale data
```

。

---

# Part 14：Non-master Region Write + Remote Marker

现在用户在：

```
Region B
```

修改自己的 profile：

```
Alice → Bob
```

实际 write 发往：

```
Master Region
```

但 Region B replica 有 replication lag。

写成功后用户刷新：

```
GET profile
```

如果：

```
Region B cache miss
↓
Region B replica DB
↓
still Alice
```

用户看到：

```
我刚改成 Bob
为什么又变 Alice？
```

这比普通 eventual consistency 更影响 user experience。

---

### Remote Marker

写 key `k` 时：

```
Step 1:
在 local region 写 remote marker rk

Step 2:
写 Master DB

Step 3:
删除 local cache k
```

后续：

```
GET k
↓
Cache miss
↓
check rk
```

如果：

```
rk exists
```

说明：

```
local replica potentially stale
```

于是：

```
不读 local replica
↓
直接去 master
```

等 replication stream 赶上并相应 invalidation 后，marker 可被清理。论文也指出 remote marker 自身存在 concurrent modification 和 eviction 等 race，因此仍不是强一致协议。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

mental model：

```
Remote Marker
=
“不要相信本地 replica” 标志
```

你可以把它看成非常廉价的：

```
per-key session-consistency hint
```

但不能把它误认为：

```
Linearizability token
```

。

---

# Part 15：State / Invariants

这篇论文没有像 Raft 那样：

```
currentTerm
log[]
commitIndex
```

这种统一 protocol state。

但可以抽象出几个关键 state。

---

### Memcache Key State

逻辑上：

```
KeyState {
    value?
    lease_token?
    recently_deleted_value?
    holdoff?
}
```

不是说真实代码一定长这样，而是帮助 reasoning。

---

### Lease invariant

最重要：

```
GET(k) → lease T

如果在之后发生：

DELETE(k)

那么：

SET(k, value, T)

不能成功。
```

如果破坏：

```
old DB read
```

可以在 writer invalidation 之后重新污染 cache。

---

### Authoritative-state invariant

```
Memcache ≠ authority
DB = authority
```

所以：

```
cache eviction
```

本身不应该造成 durable data loss。

这使系统可以非常 aggressive 地：

```
delete
expire
replace
failover
```

---

### Invalidation replay invariant

对于 committed DB mutation：

```
对应 invalidation
```

应该能够从 durable database stream 重建 / retry。

如果不能：

```
DB = new
Cache = old
```

可能长期存在。

---

### Gutter invariant

Gutter 的目标不是：

```
与 DB 严格同步
```

而是：

```
entry short-lived
```

从而给 stale data 一个有限的 exposure window。

---

# Part 16：Correctness

这一 Lecture 和 Raft 最大的不同就在这里。

不能说：

> “Facebook Memcache 保证 cache consistency。”

不准确。

更准确的是：

> **它使用一组机制显著减少 inconsistency probability，并提供 best-effort eventual consistency，同时优先保证 performance 和 availability。**

论文明确承认 delayed invalidation 会增加读取 stale data 的概率。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

## Safety

Raft safety 会说：

```
两个不同 value 不可能 committed 在同一 log index。
```

这里没有如此强的整体 safety property。

我们只能说局部 mechanism property。

例如 lease：

```
如果：

1. reader 得到 lease T
2. 对该 key 的 delete 被同一 cache server 处理
3. reader 之后用 T refill

那么：

这个 stale refill 会被拒绝。
```

这是一个很明确的 safety mechanism。

但：

```
DB update 后
DELETE 到达 cache 前
```

仍然可能有人读旧 cache。

因此：

```
Lease ≠ Linearizability
```

MIT 的考试也专门强调，简单的：

```
DB UPDATE
then DELETE
```

并不能保证 concurrent read 永远不会看到 stale value。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q22-2-sol.pdf?utm_source=chatgpt.com)

---

## Liveness / Availability

系统倾向：

```
cache unavailable
→ fallback

server failure
→ Gutter

cluster cold
→ warm cluster

regional lag
→ master

invalidations delayed
→ buffer/replay
```

所以 Facebook 经常选择：

```
继续服务
```

哪怕有一定 stale probability。

这就是：

```
availability / performance
            ↑
            |
            |
consistency ─────────────>
```

中的一个明确设计点。

---

# Part 17：Failure Matrix

|Failure|系统行为|Consistency 风险|Backend 风险|Mechanism|
|---|---|---|---|---|
|Memcached process crash|cache 丢失|低，DB 仍是真相|miss surge|Gutter|
|GET UDP packet loss|当作 miss|通常不损坏 authority|DB load 增加|fallback / Gutter|
|delayed reader SET|可能 stale set|高|低|Lease|
|hot-key invalidation|大量 simultaneous miss|较低|极高|Lease / request coalescing|
|delayed DELETE|继续读取旧 cache|stale read|较低|retry / replay|
|lost invalidation|cache 长时间 stale|高|低|mcsqueal + DB log|
|cache server failure + rehash|剩余节点被 hot key 压垮|间接|cascading failure|Gutter|
|cold cluster|大量 miss|低|DB overload|warmup|
|warm→cold stale copy|stale value 传播|高|低|delete hold-off|
|regional DB lag|local DB old|高|低|remote marker|
|whole cluster failure|cluster unavailable|—|traffic shift|reroute cluster|
|Byzantine node|不在 model 内|无保证|无保证|不处理|

---

# Part 18：一个特别值得理解的问题——为什么 Lease 不是 Lock？

你学 Distributed Lock 后很容易想：

```
lease = lock
```

不是。

Lock：

```
Client A owns key
→ others cannot manipulate protected state
```

Facebook lease：

```
Client A has permission
to fill this missing cache entry
subject to token still being valid
```

writer 不需要等：

```
lease holder
```

。

例如：

```
Reader obtains T

Writer:
DB UPDATE
DELETE
```

Writer 不 block。

只是：

```
DELETE invalidates T
```

这就是为什么这个机制性能很好。

它更像：

```
optimistic permission
+
validation
```

而不是：

```
mutual exclusion
```

。

---

## Lease 和 Fencing Token

你前面问过 Redis cache-aside fencing token，这里可以建立联系。

共同 mental model：

```
Client 拿到了 token T

之后另一个 event 发生

旧 token T 不再拥有 authority
```

例如：

```
generation = 41

client A gets 41

invalidate

generation = 42

client A SET(...,41)
→ reject
```

这就是典型的：

```
generation / epoch / fencing
```

思维。

区别是：

```
Facebook lease
主要保护 cache refill

Fencing Token
通常保护真正的 shared resource
```

。

---

# Part 19：Top 7 Misconceptions

### ❌ 1. Cache-aside + delete 就保证强一致

不保证。

时间窗口：

```
DB UPDATE
      |
      | cache still old
      v
DELETE
```

这里 read 仍可能看旧值。

而 delayed invalidation 更严重。

---

### ❌ 2. DB update 后立即 update cache 更好

并发 writer 时：

```
DB order
≠
Cache update order
```

可能导致：

```
DB newer
Cache older
```

长期存在。

---

### ❌ 3. Lease 让 Memcache Linearizable

不。

它解决：

```
specific stale-set race
+
thundering herd
```

不是完整 consistency protocol。

---

### ❌ 4. Cache server 挂了最多只是性能下降

不一定。

```
cache failure
↓
cache miss
↓
DB load
↓
DB failure
```

所以 cache failure 可以成为：

```
cascading outage
```

。

---

### ❌ 5. Consistent Hashing 自然解决 Memcache failure

rehash 可能把：

```
hot keys
```

转移给本来就繁忙的节点。

所以：

```
load distribution
```

不能只看：

```
key count
```

，还要看：

```
request frequency
```

。

---

### ❌ 6. Invalidation 越早越好

cross-region：

```
DELETE cache
```

如果比：

```
DB replication
```

快：

```
cache miss
→ stale replica DB
→ stale refill
```

反而更糟。

---

### ❌ 7. Eventual Consistency = “过一会一定自动正确”

不够精确。

你必须问：

```
什么 event 推动 convergence？
```

例如这里是：

```
DELETE propagation
retry
replay
TTL
replication catch-up
```

如果你无法指出 convergence mechanism：

> “最终一致”只是愿望，不是设计。

---

# Part 20：和 Kubernetes / Infra 联系

这个 Lecture 对 Platform Engineer 非常实用。

---

### 1. Kubernetes Controller

典型：

```
API Server
   |
Desired State
   |
Controller
   |
Cloud API
```

这里和 Cache 不完全一样。

etcd：

```
authoritative
```

controller local cache / informer：

```
derived state
```

如果 controller cache stale：

```
controller reconcile
```

必须设计成：

```
idempotent
```

，并能重新 observe authoritative state。

这和 Facebook 的：

```
DB authoritative
Cache disposable
```

mental model 很接近。

区别：

```
Kubernetes informer cache
通常有更明确的 version/resourceVersion semantics

Facebook Memcache
更弱、更 application-oriented
```

。

---

## 2. Terraform

Terraform：

```
Desired Configuration
        |
        v
Terraform State
        |
        v
Remote Cloud API
```

如果你额外在 Provider 内加入 cache：

```
DescribeResource()
```

你必须马上考虑：

```
stale observation
cache invalidation
retry
idempotency
```

特别是：

```
Create
↓
cached Describe says not found
↓
Create again
```

这种问题。

所以 Infrastructure Control Plane 通常对：

```
correctness-critical observations
```

非常谨慎地使用缓存。

---

## 3. Cloud Control Plane

假设：

```
Infra API
   |
   v
Metadata DB
   |
   v
Redis Cache
```

如果：

```
region status
endpoint config
IAM state
```

属于 correctness-critical state：

你就必须问：

```
允许 stale 多久？
```

而不是单纯：

```
“加 Redis 加速。”
```

Facebook paper 教你的真正 system design question 是：

> **staleness budget 是多少？**

---

## 4. Redis Cache-Aside

现代常见：

```
App
 |
Redis
 |
MySQL
```

完全会出现同样 stale-set race：

```
GET Redis → miss

SELECT MySQL → old

                    UPDATE MySQL new
                    DEL Redis

SET Redis old
```

所以你之前问的：

```
fencing token
version
singleflight
CDC invalidation
```

本质上就是今天这些思想的现代变种。

---

## 5. Kafka / CDC

Facebook：

```
MySQL commit
↓
mcsqueal
↓
invalidate caches
```

非常容易映射到：

```
MySQL
↓
binlog
↓
Debezium
↓
Kafka
↓
Cache Invalidation Consumer
```

但仍然要注意：

```
DB new
Cache old
```

的 transient interval。

所以：

```
CDC cache invalidation
```

通常给你：

```
eventual consistency
```

而不是：

```
immediate linearizability
```

。

---

# Part 21：这篇 Paper 真正厉害在哪里？

### Paper Problem

不是：

```
怎样让 memcached 更快？
```

而是：

```
怎样从：

one small cache cluster

扩展到：

thousands of servers
multiple frontend clusters
multiple geographic regions
billions of cache operations

同时控制：

latency
backend load
failures
consistency
operational complexity
```

论文报告当时的系统规模为每秒超过十亿次请求、存储万亿级 items。[USENIX](https://www.usenix.org/conference/nsdi13/technical-sessions/presentation/nishtala?utm_source=chatgpt.com)

---

## Previous Approach

起点其实极简单：

```
memcached =
single-machine in-memory hash table

operations:
GET
SET
DELETE
```

甚至：

```
memcached servers
```

彼此完全不知道对方存在。

---

## Key Insight

如果只能从整篇 paper 记一句设计哲学：

> **Keep memcached simple; put distributed-system intelligence around it.**

例如：

```
routing       → client/mcrouter
batching      → client
lease         → lightweight server mechanism
failure       → Gutter
replication   → pools + routing
invalidation  → DB log pipeline
geo lag       → remote marker
```

而不是造一个：

```
globally coordinated
strongly consistent
distributed cache database
```

。

这是 paper 能做到极高 scalability 的关键原因之一。[USENIX](https://www.usenix.org/system/files/conference/nsdi13/nsdi13-final170_update.pdf)

---

## Evaluation 中最值得记的几个数字

这些是 **2013 Facebook deployment 的历史结果**，不要当成现代 Memcached benchmark：

```
Popular page:
avg 521 cache items
p95 1740

Lease:
peak backend query
17K/s → 1.3K/s

Gutter:
client-visible failures
约下降 99%

Cold Cluster Warmup:
full capacity
几天 → 几小时

Invalidation batching:
median deletes / packet
≈ 18× improvement
```

这些实验并不是在证明：

```
“算法数学上正确”
```

而是在证明：

````
这些 mechanism 真正在 production scale
降低 latency / backend load / operational risk。
``` :chatgpt-content-reference{index="32"}


---

# Paper Limitations

这篇论文非常重要的一点是它并没有假装解决所有问题。

最明显的限制：

```text
没有精确强 consistency model

允许 stale read

cross-region replication lag

invalidation 可以 delay

remote marker 可以被 eviction

concurrent modifications 仍可能产生 race

cold-cluster hold-off 只是概率性保护

Gutter 故意接受 slightly stale data
````

USENIX 的公开总结也特别指出：这个系统整体 semantics 很难用一个严格 consistency guarantee 来描述，它是一种非常典型的工程 trade-off。[USENIX](https://www.usenix.org/conference/nsdi13/technical-sessions/presentation/nishtala?utm_source=chatgpt.com)

---

## What aged well?

这些思想今天仍然非常值得掌握：

```
Cache-aside
Invalidation
Request coalescing
Lease / generation tokens
Workload isolation
Routing proxy
Bulkhead / failure cache
CDC-driven invalidation
Cold-cache protection
Staleness budget
```

尤其：

> **Cache failure 不是 cache-local failure，而可能是 downstream load amplification。**

这是任何大型 control plane / backend 都适用的思想。

---

# Part 22：和你前后 Lecture 的关系

这节课真正值得放在课程里的原因，是它和前面的课程形成巨大反差。

```
Raft
↓
用 coordination
获得 strong replicated state


Spanner
↓
用 Paxos + timestamps + transaction
获得 global transactional semantics


Memcache
↓
主动避免 heavyweight coordination
接受 stale probability
换 performance / availability / simplicity
```

这其实在训练你：

> **System Design 不是看到 consistency 就上 Consensus。**

真正的问题永远是：

```
业务到底需要什么 invariant？
```

如果：

```
银行余额
```

你可能需要 strong consistency。

如果：

```
Facebook feed某个 like count
```

短暂 stale 可能完全 acceptable。

---

# Part 23：它和 COPS 的连接

你下一类 consistency 系统很自然会问：

```
如果 Linearizability 太贵
但 Facebook Memcache 又太弱

中间有没有东西？
```

当然有：

```
Causal Consistency
```

例如 COPS：

```
允许不同 region 不同步
```

但保证：

```
如果 B causally depends on A

看见 B
→ 必须看见 A
```

所以课程知识线其实很漂亮：

```
Strong consistency
       |
       | expensive
       v
Memcache:
pragmatic weak consistency
       |
       | but weak semantics hard to reason about
       v
Causal Consistency:
weaken consistency
while preserving meaningful ordering
```

---

# Part 24：这堂课没有 Raft 那样的“证明”，为什么还重要？

因为现实 Distributed Systems 有两类设计。

第一类：

```
Protocol-oriented

Raft
Paxos
2PC
```

我们喜欢问：

```
Safety theorem?
Liveness theorem?
Failure bound?
```

第二类：

```
Systems engineering-oriented

Memcache
CDN
cache
load balancer
distributed data pipelines
```

你更多问：

```
failure probability
blast radius
load amplification
staleness
latency
operability
recovery speed
```

Facebook Memcache 是后者的典型。

Staff-level System Design 非常需要这两种脑子都具备。

---

# Part 25：Problem → Solution Chain

这是本课最重要的总结。

```
Database can't handle read load
            ↓
        Add Memcache
            ↓
     Cache miss refill
            ↓
        Stale Set
            ↓
          Lease
            ↓
Many readers miss same hot key
            ↓
      Thundering Herd
            ↓
  Lease / request coalescing
            ↓
Hundreds of cache servers
            ↓
all-to-all + high latency
            ↓
 parallelism + batching
            ↓
too much parallelism
            ↓
          Incast
            ↓
      Sliding Window
            ↓
different workloads interfere
            ↓
       Memcache Pools
            ↓
hot small dataset overload
            ↓
Selective Replication
            ↓
Memcache server failure
            ↓
DB traffic explosion
            ↓
          Gutter
            ↓
multiple frontend clusters
            ↓
many copies become stale
            ↓
DB-log-driven invalidation / McSqueal
            ↓
new / restarted cluster is cold
            ↓
Cold Cluster Warmup
            ↓
warm cache can copy stale data
            ↓
Delete Hold-Off
            ↓
multiple geographic regions
            ↓
DB replica lag
            ↓
invalidation timing problem
            ↓
DB-driven delete propagation
            ↓
user writes in replica region
            ↓
read-your-write risk
            ↓
Remote Marker
            ↓
Final philosophy:

best-effort eventual consistency
+
performance
+
availability
+
operational resilience
```

如果这条链你能从头自己推出来，这堂课基本就学会了。

---

# Part 26：30 秒版本

面试官问：

> Facebook Memcache paper 讲了什么？

你可以回答：

> Facebook 将单机 memcached 发展成了一个大规模 distributed caching system。数据库仍然是 authoritative store，Memcache 使用 cache-aside 和 invalidation。主要挑战并不是存储本身，而是 stale cache、thundering herd、hot keys、cache server failures 和 multi-region replication lag。Facebook 用 lease 防止 stale refill 并合并 cache misses，用 Gutter 防止 cache failure 把数据库打爆，用 DB commit-log 驱动的 invalidation pipeline 保持多个 cache cluster 最终收敛，并通过 remote markers 减少跨 region replica lag 导致的 stale reads。整体不是 Linearizable，而是有意识地选择 best-effort eventual consistency，以换取 performance、availability 和 operational simplicity。

---

# Part 27：3 分钟版本

核心架构：

```
             Web Servers
                  |
               mcrouter
                  |
               Memcache
                  |
               cache miss
                  |
                  v
               Database
```

Read：

```
GET
↓
miss + lease
↓
DB
↓
conditional SET
```

Write：

```
DB update
↓
DELETE cache
```

lease 解决：

```
delayed stale refill
+
thundering herd
```

规模变大以后：

```
parallel fanout
→ batching
→ sliding window
```

处理 latency/incast。

不同 workload：

```
Memcache Pools
```

hot small dataset：

```
replication
```

server failure：

```
Gutter
```

multiple clusters：

```
DB commit
↓
McSqueal
↓
batched invalidations
↓
all caches
```

cold cluster：

```
warm-cache refill
+
hold-off
```

multi-region：

```
master DB
↓
replica DB
```

replication lag 导致 consistency risk，于是：

```
remote marker
→ 必要时绕过 local replica 读 master
```

最终不是强一致，而是：

```
best-effort eventual consistency
```

。

---

# Part 28：深入版本压缩

```
Problem
────────────────────────
DB can't serve huge read fanout

Model
────────────────────────
DB authoritative
Cache volatile / evictable
Network asynchronous
Crash/network/overload failures
No Byzantine

Algorithm
────────────────────────
Cache-aside
DB update + invalidate
Lease-based conditional refill
Request coalescing
Pools / replication
Gutter
DB-log-driven invalidation
Cold warmup
Remote marker

Invariants
────────────────────────
DB owns truth

Intervening DELETE
invalidates outstanding lease

Committed DB changes
drive invalidation stream

Safety
────────────────────────
Lease prevents one important
stale-set race

but system as a whole
is NOT Linearizable

Liveness
────────────────────────
fallback
retry
Gutter
warm cluster
master-region fallback

Failure Handling
────────────────────────
favor continued operation
even with bounded/probabilistic staleness

Trade-off
────────────────────────
strong consistency ↓

performance ↑
availability ↑
operational simplicity ↑
```

---

# Part 29：知识网络

最后把今天的知识挂起来：

```
                         Distributed Systems
                                  |
              +-------------------+-------------------+
              |                                       |
       Strong State                              Derived State
              |                                       |
      Replication                                 Caching
              |                                       |
      Consensus                                Cache-Aside
       /      \                                      |
    Raft      Paxos                              Invalidation
              |                                      |
          Spanner                                  Lease
                                                     |
                                      +--------------+--------------+
                                      |              |              |
                                  stale set    thundering herd   hot key
                                      |
                               Cache Consistency
                                      |
                    +-----------------+-----------------+
                    |                 |                 |
                  Gutter            Pools         McSqueal/CDC
                    |                                   |
             Fault Isolation                         Region
                                                        |
                                                Replica DB Lag
                                                        |
                                                Remote Marker
                                                        |
                                           Best-effort Eventual
                                               Consistency
```

---

## 最后给你 5 道真正值得做的题

#### Level 1 — Execution

初始：

```
DB x=1
Cache empty
```

```
R: GET → MISS + lease T
R: DB.read → 1

W: DB.write x=2
W: DELETE x

R: SET x=1,T
```

回答：

```
最后 Cache 是什么？
为什么？
```

---

#### Level 2 — Counterexample

如果我们删除 Lease，改成：

```
cache miss
↓
DB read
↓
SET
```

请构造一个 execution：

```
DB = 200
Cache = 100
```

并且 cache 可以长期保持 `100`。

---

#### Level 3 — Failure

Memcache Server A 挂了，系统有：

```
B
C
D
```

为什么：

```
rehash A's keys to B/C/D
```

可能比：

```
Gutter
```

危险？

关键不要回答：

```
“因为 Gutter 是备用。”
```

而要从：

```
hot-key load distribution
```

解释。

---

#### Level 4 — Geo Consistency

假设：

```
Master DB = x=2
Replica DB = x=1
Replica Cache = x=1
```

为什么：

```
Master 立即向 Replica Region
DELETE cache[x]
```

**反而可能增加** stale-value 被重新缓存的概率？

这是本课最值得会推理的一道题。

---

#### Level 5 — System Design

现在让你设计：

```
Zilliz Cloud metadata

MySQL
+
Redis
+
3 Regions
```

有一个 key：

```
project/123/status
```

要求：

```
普通 list API：
允许 5 秒 stale

删除 Project：
绝不能因为 stale cache
让后续操作重新操作已删除资源
```

你需要开始区分：

```
哪些读可以 eventual

哪些 path 不能信 cache

是否使用 invalidation

是否使用 version/fencing token

是否用 CDC

是否需要直接读 authoritative DB
```

这就是把今天这堂 Memcache lecture 真正转化成 **Staff-level system design reasoning** 的那一步。