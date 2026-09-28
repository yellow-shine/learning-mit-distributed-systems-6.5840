你这节课先抓住一个非常重要的前提：

> **Amazon DynamoDB ≠ 2007 年的 Amazon Dynamo。**

MIT 这节课对应的核心材料是 2022 USENIX 的 _Amazon DynamoDB: A Scalable, Predictably Performant, and Fully Managed NoSQL Database Service_。论文甚至明确说：DynamoDB 虽然沿用了 Dynamo 的名字，但 **“little of its architecture”**；现代 DynamoDB 的单 Region Replication 使用 **Multi-Paxos、Leader、Quorum 和 WAL**，并同时提供 Strongly Consistent Read 与 Eventually Consistent Read。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

这点特别重要：**不要拿“Dynamo = Vector Clock + Sloppy Quorum + Eventual Consistency”那套 mental model 直接套 DynamoDB。**

---

# Part 1：如果这节课只能记住一个问题

我希望你记住的问题不是：

> DynamoDB 怎么做 Key-Value Store？

而是：

> **怎样构建一个巨型 multi-tenant database service，使它在数据量、请求量、partition 数量、机器数量不断增长，并且机器持续失败、流量高度不均匀的情况下，仍然让每个用户感觉自己在使用一台低延迟、高可用、性能可预测的数据库？**

关键词其实是：

```
Scale
+
Multi-tenancy
+
Failures
+
Skewed workload
+
Predictable performance
+
High availability
```

2022 年论文强调的目标不是单纯追求最高 throughput，而是：

> **predictability**

即从小表扩大到数百 TB、从较低流量扩展到极高流量时，延迟不要突然恶化。论文给出的生产目标是 low single-digit millisecond latency，并强调 tail latency / unpredictable latency 对上层服务的影响。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

---

# 1. 系统现在面临什么问题？

假设 Amazon 给你一个需求：

```
Customer A: 1 GB table,       100 req/s
Customer B: 10 TB table,      1,000,000 req/s
Customer C: 200 TB table,     highly skewed traffic
Customer D: burst traffic     100x suddenly

                   ↓

              DynamoDB

     +-------------+-------------+
     |             |             |
 Storage A      Storage B      Storage C
     |             |             |
    SSD           SSD           SSD
```

而这些客户不是一人一套数据库。

DynamoDB 是 **multi-tenant**：

```
Storage Node X

Partition A1  <- Customer A
Partition B7  <- Customer B
Partition C3  <- Customer C
Partition D9  <- Customer D
...
```

同一台物理机器可能承载大量不同 table 的 partition replicas。这样资源利用率高，但马上出现一个非常现实的问题：

```
Customer C 突然产生巨大流量
            ↓
Storage Node CPU / disk / network 被打满
            ↓
Customer A 的请求 latency 也升高
```

也就是经典：

> **noisy neighbor problem**

论文明确指出 DynamoDB 的 multi-tenant 架构必须同时做到资源共享和 workload isolation。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

于是 DynamoDB 面临的并不仅仅是：

```
如何把数据复制三份？
```

而是：

```
怎么放数据？
怎么路由请求？
怎么复制？
怎么 failover？
怎么防止一个 tenant 搞死别人？
怎么处理 hot partition？
怎么自动 split？
怎么让 partition 移动时不中断服务？
怎么处理机器慢而不是彻底死？
怎么发现 silent corruption？
怎么升级几万台机器而不中断？
```

这才是这节课真正有意思的地方。

---

# 2. 为什么单机数据库里这个问题简单很多？

假设只有：

```
Application
     |
     v
 PostgreSQL
     |
    SSD
```

你有一个 key：

```
user:123 → Alice
```

`Put(user:123, Alice)`：

```
WAL append
   ↓
page update
   ↓
return OK
```

很多事情其实非常明确：

```
数据在哪里？

→ 就在这台机器。

谁负责这个 key？

→ 这台机器。

现在谁是 leader？

→ 不存在这个问题。

另一台机器上的 copy 是否已经收到？

→ 没有另一台机器。

哪个 partition？

→ 甚至可能不需要 partition。

metadata 应该把请求 route 到哪里？

→ localhost。
```

甚至 capacity 问题也比较直接：

```
机器极限 = 100k req/s

现在 = 90k req/s

快不够了
```

---

# 3. 一进入 Distributed System，事情为什么突然变难？

现在：

```
                     Request Router
                           |
                           v

             hash(partition key)
                           |
                           v
                   Partition P17

              Replication Group

             AZ1       AZ2       AZ3
              |         |         |
              v         v         v
              A         B         C
           Leader   Follower  Follower
```

DynamoDB 中，一个 table 被切成很多 partition，每个 partition 覆盖一个不重叠的 key-range；partition 有多个 replica，分布在不同 Availability Zones。一个 replication group 使用 Multi-Paxos 做 leader election / consensus。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

于是一次：

```
Put(user:123, Alice)
```

背后已经不是：

```
写硬盘
```

而变成：

```
1. user:123 属于哪个 partition？
2. partition 当前有哪些 replica？
3. leader 是谁？
4. leader 还活着吗？
5. metadata 是最新的吗？
6. leader 把 WAL 写到哪些 replica？
7. 写到几个才算 committed？
8. 某个 AZ 挂了怎么办？
9. follower 很慢怎么办？
10. leader crash 怎么办？
11. 新 leader 怎么确保不会和老 leader 同时写？
```

DynamoDB 当前论文模型中，典型 partition 有 3 个跨 AZ replica；写请求由 leader 处理，WAL 被复制并在一个 quorum 持久化后才向 client 确认。论文明确给出的 healthy write quorum 是 **3 个 replica 中的 2 个**。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

也就是：

```
Client
  |
  | Put(x=1)
  v
 Leader A
  |
  | WAL(x=1)
  +----------> Replica B
  |
  +----------> Replica C


A persisted ✓
B persisted ✓
C slow      ...

       ↓

quorum reached

       ↓

Client receives OK
```

这里已经出现你学过的：

```
Replication
Consensus
Quorum
Leader
WAL
Crash recovery
Failure detector
Lease
```

但这还只是**第一层问题**。

---

# 4. 最 naive 的方案是什么？

看到 table 太大，你很自然会想到：

> Hash 后平均分区。

例如 table provision：

```
300 RCU
```

有三个 partition：

```
P1 = 100 RCU
P2 = 100 RCU
P3 = 100 RCU
```

看起来完美：

```
                300 RCU table

             /      |      \
          P1       P2       P3
        100 RCU  100 RCU  100 RCU
```

这背后的隐含假设是：

```
request distribution ≈ uniform
```

比如：

```
P1: 90 req/s
P2: 80 req/s
P3: 70 req/s
```

没问题。

---

# 5. Naive 方法在哪里真正翻车？

真实 workload 很少这么漂亮。

假设：

```
table provisioned = 300 RCU

P1 = 100 RCU
P2 = 100 RCU
P3 = 100 RCU
```

实际流量：

```
P1: 250 req/s   🔥
P2:  10 req/s
P3:  10 req/s
```

整个 table 实际只用了：

```
250 + 10 + 10 = 270 RCU
```

明明：

```
270 < 300
```

但 P1：

```
250 > 100
```

于是：

```
P1 → throttle
```

客户看到：

```
Provisioned: 300
Used:        270

为什么还 throttle？？？
```

这正是 DynamoDB 早期设计碰到的核心工程问题之一。论文将这类现象归为 **hot partitions** 和 **throughput dilution**：请求通常并不均匀分布在 key-space 或时间轴上，因此把 table capacity 静态平均分配到 partitions，会导致明明 table 总 capacity 足够，某个 hot partition 却被 throttling。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

这也是 MIT 这节课非常值得学的地方。

因为：

> **Sharding 解决了“怎么横向扩容”，但它没有自动解决“流量是否均匀”。**

这是 System Design 面试里非常重要的一层。

---

# 一个更反直觉的 Failure：split 之后反而更慢

这是 MIT 特别喜欢考的场景。

原来：

```
             P1
          1000 RCU
```

用户流量：

```
hot keys → 800 RCU
```

没问题。

后来因为数据太大，partition split：

```
             P1
           /    \
         P1a    P1b

       500 RCU 500 RCU
```

但是 hot keys 恰好基本都去了 P1a：

```
P1a traffic = 800
capacity    = 500

P1b traffic = 0
capacity    = 500
```

于是：

```
Before split:
800 / 1000
✓

After split:
P1a = 800 / 500
✗ throttling
```

注意：

> **数据 split 成两半，不代表 traffic 也 split 成两半。**

论文指出早期 DynamoDB 在因为 size 做 partition split 时，会把 throughput 在 child partitions 之间分配；但实际访问往往 skewed，因此 split 反而可能让 hot portion 获得更少 capacity。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

这个例子值得你牢牢记住。

---

# 所以 DynamoDB 真正要解决的不是一个问题，而是一条链

先只看这个链，不看具体机制：

```
一个节点放不下
    ↓
Partition / Sharding

但 partition 会失败
    ↓
Replication

但 replicas 必须决定哪个 write 生效
    ↓
Leader + Multi-Paxos + Quorum

但 replica / AZ 会失败
    ↓
Failover + Leader Election

但 failure detector 会误判
    ↓
Lease + gray failure handling

但 partition 的访问不均匀
    ↓
Bursting / Adaptive Capacity

但这些机制仍然是 reactive / local
    ↓
Global Admission Control

但一个 partition 本身真的太热
    ↓
Split for consumption

但机器本身也会过载
    ↓
Replica placement / migration / balancing

但持久化三份仍可能一起出现数据错误
    ↓
WAL + checksum + S3 archive
    + continuous verification

但 metadata service 自己也可能成为瓶颈
    ↓
Distributed metadata architecture

但这些东西全都要升级
    ↓
Safe deployment / rollback / compatibility
```

**这才是这篇 DynamoDB paper 的故事。**

它不是介绍一个漂亮的单一算法，而是在回答：

> 一个分布式数据库从“算法正确”走到“十年持续运行在巨大生产规模”，还要补上哪些机制？

论文也明确把经验总结为几类：根据真实 traffic reshaping partitions、持续验证 data-at-rest、通过 formal methods / failure injection / deployment discipline 保持高 availability，以及优先追求 predictable behavior 而非只追求局部效率。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

---

# 一个必须现在建立起来的 Mental Model

你之前学 Raft 时，重点通常是：

```
有 3/5 台 server

如何让大家对：

log[100] = Put(x,1)

达成一致？
```

DynamoDB 把问题扩大了好几个数量级：

```
Table A
 |
 +-- Partition 1 → Paxos Group #1
 |
 +-- Partition 2 → Paxos Group #2
 |
 +-- Partition 3 → Paxos Group #3
 |
 ...
 |
 +-- Partition 1,000,000 → Paxos Group #1,000,000
```

论文描述 DynamoDB 一个 Region 中可以运行 **millions of Paxos groups**。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

所以问题从：

> “Consensus 怎么工作？”

升级成：

> **“如果我要同时运营数百万个 Consensus group，怎么让整个数据库仍然可扩展、可调度、可升级、可恢复、性能稳定？”**

这正好是你从学习 **algorithm** 进入学习 **distributed system architecture** 的分界线。

---

# Part 2：它在整个 6.824 知识地图里的位置

可以先把这一课放成这样：

```
                    Distributed Systems
                            |
          +-----------------+------------------+
          |                                    |
     Correctness                           Scalability
          |                                    |
    Consensus / Raft                       Sharding
          |                                    |
   Replicated State                     Load Balancing
          |                                    |
          +----------------+-------------------+
                           |
                       DynamoDB
                           |
          +----------------+----------------+
          |                |                |
      Replication       Partitioning   Multi-tenancy
          |                |                |
      Multi-Paxos       Hot Keys      Admission Control
          |                |                |
       Quorum          Rebalancing      Isolation
          |
   Strong / Eventual Reads
```

---

## 前面学的 Raft / Paxos 在这里解决什么？

Raft / Paxos 回答：

> 给定一个 replication group，怎样在机器 crash、网络 delay 等情况下，对 operation order 达成 Consensus？

简化：

```
Replica A
Replica B
Replica C

       ↓ Paxos

Put(x=1)
Put(y=2)
Put(x=3)

大家最终同意这个顺序
```

DynamoDB **没有取代 Consensus**。

恰恰相反：

```
DynamoDB
    ↓
每个 partition
    ↓
Replication Group
    ↓
Multi-Paxos
```

论文明确说明每个 partition 的 replication group 使用 Multi-Paxos 做 leader election 和 consensus。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

所以边界是：

```
Paxos
解决：
一个 replica group 内部如何一致。

DynamoDB
还必须解决：
有海量 replica groups 时，
它们怎么 partition / route / scale / rebalance / repair /
isolate / deploy / monitor。
```

---

# Raft/Paxos 与 Sharding 是两个正交问题

这是一个极重要的 mental model。

假设：

```
Key space:

A--------------------------------Z
```

Sharding：

```
A-H → Partition 1
I-Q → Partition 2
R-Z → Partition 3
```

回答：

> **哪个 group 管这个 key？**

Replication：

```
Partition 1:

Replica A
Replica B
Replica C
```

回答：

> **这个 group 如何容忍机器故障？**

Consensus：

```
A/B/C
```

回答：

> **replicas 对 mutation 顺序怎么达成一致？**

因此：

```
Sharding
=
scale-out across groups

Replication
=
duplicate data within a group

Consensus
=
coordinate replicas within that group
```

三者解决的是不同维度。

---

# State Machine Replication 与 DynamoDB

经典 State Machine Replication：

```
commands
   ↓
Consensus Log
   ↓
State Machine
   ↓
same state
```

例如：

```
log:
1 Put(x, 1)
2 Put(y, 2)
3 Delete(x)
```

DynamoDB 的 partition-level replication 可以用类似 mental model 理解：

```
                   Partition P

                        |
               Replication Group
                        |
                    Multi-Paxos
                        |
                  ordered WAL
                        |
                       B-tree
```

但不要过度等同。

论文描述的是生产级 database replication：

```
WAL
B-tree
storage replicas
log-only replicas
leader lease
repair
S3 log archive
checksums
scrubbing
```

所以：

> State Machine Replication 给你 correctness abstraction；DynamoDB 展示怎么把它变成真正的大规模 storage service。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

---

# Linearizability 在这里在哪里？

这会是后面非常关键的一点。

DynamoDB 支持：

```
Strongly Consistent Read
```

以及：

```
Eventually Consistent Read
```

论文中的架构是：

```
               Leader
              /      \
             /        \
        Replica       Replica


Strong read
    ↓
Leader


Eventual read
    ↓
Any replica
```

只有 leader 提供 write 和 strongly consistent read；eventually consistent read 可以由任意 replica 提供。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

为什么？

后面我们详细推 timeline。

但现在先建立直觉：

```
Leader:

x = 2

Follower:

x = 1
```

如果：

```
Get(x)
```

随便问 follower：

```
return 1
```

那自然可能是 stale read。

所以：

```
更强 consistency
          ↕
更多 coordination

更低 latency / 更高 read availability
          ↕
允许 stale state
```

这条轴就是你之前学过的 Linearizability 真正在生产系统里的落点。

---

# DynamoDB 与 Spanner 的区别

你学过 Spanner 后，很容易问：

> 两个不都是 replicated distributed database？

是，但目标非常不同。

可以粗略这样理解：

```
Spanner
重点：
global transactions
external consistency
distributed SQL-ish database
TrueTime
cross-shard transaction correctness


DynamoDB
重点：
key-value / document access
predictable low latency
massive horizontal scaling
multi-tenancy
operational simplicity
partition elasticity
```

这不代表 DynamoDB 没有 transaction——现代 DynamoDB 有，而且论文明确支持跨 items 的 ACID transaction。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

区别在于：

> **它们从一开始优化的中心问题不同。**

Spanner 那一课更像：

```
怎样让：

New York
London
Singapore

一起做强一致 transaction？
```

DynamoDB 这篇更像：

```
怎样让：

millions of partitions
millions of Paxos groups
huge multi-tenant fleet

长期稳定运行，
而用户不用管机器？
```

---

# DynamoDB 与 ZooKeeper 的区别

ZooKeeper：

```
small coordination state
       +
strong ordering / consistency
       +
watch / ephemeral node
```

典型：

```
leader election
service discovery
configuration
locks
```

DynamoDB：

```
application data
+
huge storage scale
+
huge traffic scale
+
partitioned database
```

所以不要想：

```
ZooKeeper = 小 DynamoDB
```

而应该理解：

```
ZooKeeper
优化的是：
coordination semantics

DynamoDB
优化的是：
large-scale data storage + serving
```

---

# DynamoDB 与 Distributed Transaction

这里有两层：

```
Level 1:

Put(k,v)

只修改一个 item
       ↓
单 partition replication / consensus
```

和：

```
Level 2:

Transfer $50:

Mary -= 50
Bob  += 50

可能跨 partition
       ↓
Distributed Transaction
```

后一种不能仅靠：

```
每个 partition 自己 Paxos 正确
```

就推出：

```
跨两个 partition atomic
```

这是你前面学 2PC / Distributed Transactions 时最重要的边界：

> **Consensus inside each shard ≠ Atomic transaction across shards.**

现代 DynamoDB 的 Transactions 是额外的一层协议，这节课的 slides 也专门把 multi-item invariants 引出来；2022 论文指出 DynamoDB 提供 ACID transactions，而后续还有单独的 DynamoDB distributed transactions paper。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/DynamoDB-MIT-Lecture-2023.pdf)

---

# DynamoDB 与 Distributed Cache 的区别

比如 Redis cache：

```
Application
     |
     v
   Redis
     |
     v
 Database
```

通常 cache 可以：

```
miss
evict
expire
```

其核心目标是：

```
performance optimization
```

DynamoDB 是：

```
system of record
```

如果：

```
DynamoDB Put(x=1)
→ return success
```

那 durability 是数据库 contract 的一部分。

所以论文才强调：

```
committed data
should never be lost
```

并为此增加：

```
WAL
multiple AZ replicas
S3 log archive
checksums
continuous scrub
backup
PITR
```

而不只是：

````
“复制三份就完了。”
``` :chatgpt-content-reference{index="14"}


---

# DynamoDB 与 Kubernetes / Cloud Control Plane

这部分和你的 Infra 背景其实非常好联系。

DynamoDB 架构中存在：

```text
                    Request path

Client
   |
   v
Request Router
   |
   | partition metadata
   v
Storage Replica Group
````

另一边：

```
                    Control plane

               AutoAdmin
                  |
       +----------+-----------+
       |          |           |
    monitor     repair      rebalance
       |          |           |
       v          v           v
   replicas    nodes      partitions
```

论文把 `autoadmin` 称为 DynamoDB 的核心控制组件，它负责 fleet health、partition health、table scaling、replica replacement 等。[USENIX](https://www.usenix.org/system/files/atc22-elhemali.pdf)

这和 Kubernetes 很容易建立一个**有限但很有用的类比**：

```
Kubernetes

Desired State
     |
     v
Controller
     |
 observe
     |
     v
Actual State
     |
 reconcile
     v
Desired State
```

DynamoDB：

```
Desired:
partition should have healthy replicas

Actual:
one storage node died

       ↓

AutoAdmin observes

       ↓

create / move / repair replica

       ↓

healthy replication restored
```

但差异也必须说清楚：

```
Kubernetes Controller
通常管理 general-purpose resources

DynamoDB AutoAdmin
是针对 database fleet/partition/replica
高度专用的 control plane
```

后面看到：

```
partition split
replica migration
log replica
failure recovery
```

你可以一直用这种：

> **reconciliation / desired-state**

思维去理解。

---

# 到这里先压缩成一句话

现在不要记：

```
RCU
WCU
GAC
Token Bucket
Log Replica
MemDS
...
```

这些都先放一边。

你现在应该形成的第一层 mental model 是：

```
                 DynamoDB

                      ↓

“Consensus 算法已经有了，
那怎么把几百万个 replicated partitions
变成一个稳定、低延迟、自动运维、
multi-tenant 的数据库服务？”

                      ↓

 Partitioning
      +
 Replication
      +
 Consensus
      +
 Admission Control
      +
 Rebalancing
      +
 Failure Recovery
      +
 Continuous Verification
      +
 Control Plane
```

**Raft/Paxos 解决的是一个 replication group 的正确性。**

**DynamoDB 这一课真正往前迈的一步，是教你怎样运营海量这样的 group，同时处理真实世界的 skew、gray failure、hardware corruption、metadata bottleneck 和 deployment。**

这也是为什么这篇 paper 对有 Platform / Infrastructure 背景的人特别值得学。[USENIX](https://www.usenix.org/conference/atc22/presentation/elhemali?utm_source=chatgpt.com)

---

## 理解检查

先不要查定义，直接推理下面三个场景。

**问题 1**

有一个 DynamoDB table：

```
Total capacity = 300 RCU

P1 = 100
P2 = 100
P3 = 100
```

实际：

```
P1 = 220 req/s
P2 = 20 req/s
P3 = 20 req/s
```

总请求只有：

```
260 < 300
```

为什么 P1 仍然可能 throttle？

更重要的是：

> 这说明了“table capacity”和“partition capacity”之间存在什么结构性问题？

**问题 2**

假设：

```
P1 capacity = 1000

traffic:
hot key range = 800
cold key range = 100
```

因为 size 太大，P1 被平分：

```
P1a = 500
P1b = 500
```

如果所有 hot keys 都进入 P1a：

```
P1a = ?
P1b = ?
```

为什么 **“增加 partition 数量”反而可能降低 availability？**

**问题 3**

你已经有：

```
1000 个 partitions

每个 partition：
3 replicas
Raft/Paxos replication
```

是否就已经拥有一个 DynamoDB？

如果不是，请你至少说出 **3 个 Consensus 完全没有替你解决的问题**。

你回答这三个之后，下一块我们进入最核心的 **Part 3–7**：

```
DynamoDB Architecture
        ↓
Partition
        ↓
Replication Group
        ↓
Multi-Paxos
        ↓
Leader + Lease
        ↓
WAL + Quorum
        ↓
Strong Read vs Eventual Read
        ↓
Log Replica
```

我会用完整 timeline 推一次：

```
Put(x=1)
→ quorum
→ leader crash
→ failure detection
→ leader election
→ old lease expiry
→ new leader serves traffic
```

并在每一步问：**“如果这里 crash 呢？”**