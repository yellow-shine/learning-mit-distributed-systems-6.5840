## MIT 6.824 Lecture 3 — GFS

先把这一课压缩成一句话：

> **GFS 真正解决的不是“怎么把一个文件存到很多机器”，而是：当机器故障是常态、数据巨大、客户端很多时，如何把 metadata、sharding、replication、mutation ordering、failure recovery 组合成一个可扩展的 Distributed Filesystem。**

这篇 2003 年的 GFS paper 是 6.824 很关键的一课。它不是先发明一个漂亮的 consensus algorithm，而是从 Google 当时真实 workload 出发：普通机器会坏、文件很大、append 多、随机 overwrite 少、bulk throughput 比单次低 latency 更重要，然后反推整个系统架构。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 1：这节课到底想解决什么问题？

想象 Google 有：

```
10,000 个 worker
1000 台 storage server
数百 TB / PB 数据
大量 crawler / indexing / MapReduce job
```

如果还是传统文件服务器：

```
Clients
   |
   v
File Server
   |
   v
Disk
```

单机里很多事情很简单：

```
文件名 -> inode -> disk block
```

机器只有一个 authoritative state，写：

```
write(file, offset, data)
```

操作系统决定写到哪块 disk。

但进入 distributed system：

```
                   ????? metadata ?????
                          |
             +------------+------------+
             |            |            |
             v            v            v
         Server A      Server B      Server C
```

突然出现一连串问题：

```
文件切成什么单位？
每一块在哪？
谁记录 location？
数据复制几份？
replica 怎么保持一致？
两个 client 同时写怎么办？
server crash 怎么恢复？
metadata server crash 怎么办？
disk bit corruption 怎么发现？
新增机器怎么 rebalance？
```

最 naive 的方案是：

```
每个文件复制 3 份。
Client 同时写三个副本。
```

但马上炸掉。

例如：

```
Client A: write X at offset 100
Client B: write Y at offset 100
```

可能出现：

```
Replica 1: X
Replica 2: Y
Replica 3: X
```

Replication 解决了：

> “一台机器坏了还有数据。”

但没有解决：

> “多个 replica 如何形成一个 coherent system。”

这正是 GFS 开始有意思的地方。

---

# Part 2：放到整个 6.824 知识地图里

你可以这样理解课程前几节：

```
Distributed Systems
        |
        +-- Lecture 1
        |     为什么需要 distributed system
        |
        +-- Lecture 2
        |     RPC / Threads
        |     节点之间怎么通信
        |
        +-- Lecture 3
              GFS
              |
              | 把前面的零件组合起来
              v
       一个真正的大规模系统
```

之后：

```
                    GFS
                     |
       +-------------+---------------+
       |             |               |
       v             v               v
 Replication    Fault Tolerance   Coordination
       |                             |
       v                             v
Primary/Backup                    Raft/Paxos
                                     |
                                     v
                              ZooKeeper / etcd
```

最重要的是边界。

#### RPC vs GFS

RPC 回答：

> A 怎么调用 B？

GFS 回答：

> 用 RPC 连接几千台机器之后，整个 storage system 怎么组织？

---

#### Replication vs GFS

Replication 是机制：

```
Data
 |
 +--> A
 +--> B
 +--> C
```

GFS 是完整系统设计：

```
partition
+ replication
+ metadata
+ mutation ordering
+ recovery
+ checksums
+ rebalancing
+ GC
```

---

#### Consensus / Raft vs GFS

Raft 解决：

> 一组 replicas 怎样对 replicated state machine 的 command 顺序达成一致。

GFS 的重点是：

> 大规模数据如何存储。

这是一个极其重要的区别。

```
Raft:
tiny-ish replicated state
+
strong ordering

GFS:
huge bulk data
+
metadata coordination
+
replicated chunks
```

而且 **GFS paper 中的 Master 并不是一个 Raft cluster**。

---

#### Distributed Transaction vs GFS

Distributed Transaction：

> 多个 independent data item / shard 的修改怎么保持 transaction semantics。

GFS：

> 文件块怎么存、复制、读写、恢复。

GFS 不试图提供完整数据库 transaction abstraction。

---

#### MapReduce / Spark vs GFS

这是最自然的一组关系：

```
                MapReduce / Spark
                      |
                    Compute
                      |
                      v
             huge dataset access
                      |
                      v
                     GFS
```

MapReduce 的 input/output 可以存在 GFS；MapReduce 论文明确描述了其实现从 GFS 读取输入并把输出写回 GFS。[Google Research](https://research.google.com/archive/mapreduce-osdi04.pdf?utm_source=chatgpt.com)

所以可以把它们理解为：

```
GFS       = distributed storage layer
MapReduce = distributed compute layer
```

---

# Part 3：先建立完整 Architecture Mental Model

原始 GFS 非常漂亮的一点是：

> **Control Plane 和 Data Plane 分离。**

基本架构：

```
                          +----------------+
                          |     Master     |
                          |                |
                          | namespace      |
                          | file -> chunks |
                          | locations      |
                          | leases         |
                          +-------+--------+
                                  ^
                                  |
                           metadata RPC
                                  |
                                  |
+-------------+                   |
| Application |                   |
+------+------+                   |
       |                          |
       v                          |
+-------------+                   |
| GFS Client  |-------------------+
+------+------+
       |
       | actual file data
       |
       +----------+-----------+
       |          |           |
       v          v           v
 +-----------+ +-----------+ +-----------+
 |Chunkserver| |Chunkserver| |Chunkserver|
 |     A     | |     B     | |     C     |
 +-----------+ +-----------+ +-----------+
       |            |             |
    local disk   local disk    local disk
```

GFS 的 Master 管 metadata，但 **实际文件数据不经过 Master**。Client 从 Master 获取 chunk handle 和 replica locations 后，直接和 Chunkserver 通信。这样避免让 Master 成为 bulk data bottleneck。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这是整课最重要的 architecture pattern 之一：

```
Control Plane
    ≠
Data Plane
```

你在 Cloud / Kubernetes 系统里应该非常熟悉这个思想。

---

# Part 4：为什么是 Chunk？

假设一个文件：

```
/data/crawl/result
size = 1 TB
```

不可能合理地要求：

```
1 TB file
=
一个 placement unit
```

于是切分：

```
File F

Chunk 0
Chunk 1
Chunk 2
Chunk 3
...
```

原始 GFS 使用固定 **64 MB chunk**，远大于当时传统文件系统 block。每个 chunk 有 Master 分配的全局唯一 64-bit handle，Chunkserver 把 chunk 当成普通 Linux file 存在本地磁盘；默认 replica 数量为 3。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

所以：

```
File
  |
  v
logical chunks
  |
  +------ Chunk 0 ------> A B C
  |
  +------ Chunk 1 ------> B D E
  |
  +------ Chunk 2 ------> A D F
```

这里出现三个不同概念：

```
File
=
用户看到的 logical object

Chunk
=
GFS 的 storage / replication unit

Linux file
=
Chunkserver 本地实际存储形式
```

---

### 为什么 Chunk 很大？

假设：

```
1 TB file
```

如果 chunk = 4 KB：

```
约 268 million chunks
```

Master 的 metadata 会爆炸。

如果 chunk = 64 MB：

```
1 TB / 64 MB
≈ 16,384 chunks
```

大 chunk 有三个主要好处：

```
更少 metadata
更少 client -> master lookup
更容易复用 client -> chunkserver connection
```

论文明确把这些作为 64 MB large chunk 的重要收益。代价则包括 small file/hot chunk 容易形成热点。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这就是经典 trade-off：

```
large chunk
   |
   +--> metadata ↓
   +--> control RPC ↓
   +--> sequential throughput ↑
   |
   +--> hotspot risk ↑
   +--> load-balancing granularity ↓
```

---

# Part 5：第一个核心机制——Single Master

现在问题来了：

```
File /foo/bar
offset 200 MB
```

Client 怎么知道去哪读？

所以必须有 metadata：

```
/foo/bar
   |
   +-> Chunk 0
   +-> Chunk 1
   +-> Chunk 2
             |
             +-> Server A
             +-> Server D
             +-> Server F
```

GFS 把这些 global metadata 放在 Master。

Master 维护的主要内容包括：

```
namespace

file -> chunks mapping

chunk -> replicas locations

chunk version

lease information
```

但这里有一个很漂亮的设计。

#### 什么东西 persistent？

Namespace 和：

```
file -> chunk mapping
```

是 authoritative state，需要通过 operation log 持久化。

但：

```
chunk -> chunkserver location
```

**不需要持久化。**

Master restart 时直接问：

```
Chunkserver A:
你有什么 chunks？

Chunkserver B:
你有什么 chunks？

...
```

重新 reconstruct。

论文明确采用这种方法，因为机器不断 join、leave、restart、disk failure，Master 上持久保存 replica location 反而容易和现实世界失去同步。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这个设计思想非常值得记：

> **能从 reality 重建出来的 derived state，不一定值得作为 authoritative persistent state。**

---

# Part 6：Read Happy Path

假设：

```
Read(
  file="/foo/bar",
  offset=130MB
)
```

chunk size = 64 MB。

那么：

```
chunk index
=
floor(130 / 64)
=
2
```

Client：

```
            Master

Client ---- file="/foo/bar"
           chunk=2
             --->

       <--- chunkHandle=0xABC
            replicas=[A,C,F]
```

然后：

```
Client ------ Read(handle=ABC, range=...) -----> A
```

注意：

```
Master
```

已经退出 data path。

完整流程：

```
1. client 根据 offset 算 chunk index

2. client 问 master：
   file + chunk index -> handle + replica locations

3. client cache metadata

4. client 选一个 replica
   通常选网络上较近的

5. client 直接读 chunkserver
```

这是 GFS scalability 的根基。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 7：这里为什么不 cache 文件数据？

传统 filesystem 很喜欢 client cache。

但 GFS workload 是：

```
huge files
+
streaming reads
+
working set huge
```

假设：

```
300 GB dataset
```

Client cache 512 MB：

```
read 300 GB sequentially
```

几乎不会再次访问刚读的 block。

Cache hit rate 很差。

反而你会引入：

```
cache invalidation
cache coherence
stale cache
```

所以 GFS：

```
client caches metadata
```

但基本不做：

```
client-side file-data cache
```

Chunkserver 本身也依赖 Linux buffer cache。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这是 workload-driven design 的典型例子：

> 好的 abstraction 不是越多功能越好，而是根据 workload 删除不值得的复杂度。

---

# Part 8：Write 才是真正困难的部分

Read 很简单：

```
挑一个 replica 读
```

Write 不一样。

假设 replicas：

```
A
B
C
```

两个 Client：

```
Client 1: Write(X)
Client 2: Write(Y)
```

如果各自并行发送：

```
         A       B       C

C1       X ----> X ----> X
C2       Y ----> Y ----> Y
```

网络延迟不同可能得到：

```
A: X,Y
B: Y,X
C: X,Y
```

三个 replica diverge。

所以需要一个新的机制：

> **所有 replicas 必须以相同顺序执行 mutations。**

---

# Part 9：Lease + Primary

GFS 的做法：

```
Master
   |
   | lease
   v
Replica A
= Primary

Replica B
= Secondary

Replica C
= Secondary
```

注意这个 Primary 是：

> **这个 chunk 的 Primary。**

不是整个 GFS Master。

Primary 负责：

```
决定 mutation ordering
```

比如：

```
Client1 mutation X
Client2 mutation Y
```

Primary 决定：

```
serial #100 = X
serial #101 = Y
```

然后所有 replicas：

```
A: X Y
B: X Y
C: X Y
```

Master 给某 replica 一个 lease；原论文 lease 初始 timeout 是 60 秒，可以通过 HeartBeat 延长。Master 失去 Primary 联系后，不会马上随便指定另一个，而是要等待旧 lease 安全过期。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

Mental model：

```
Master
=
决定“谁有 ordering authority”

Primary
=
决定“具体 mutation 顺序”
```

---

# Part 10：一次 Write 到底发生什么？

这是整节课最值得掌握的 timeline。

假设：

```
Primary   = A
Secondary = B,C
```

#### Step 1：找 Primary

```
Client ---> Master

"这个 chunk 谁持有 lease？"
```

Master：

```
Primary = A
Secondaries = B,C
```

---

#### Step 2：先传播 data

这里是 GFS 一个很漂亮的设计。

Client 不先发：

```
Write(offset=100)
```

而是先把：

```
data = "hello..."
```

传播出去：

```
Client ---> A ---> B ---> C
```

每个 Chunkserver 暂存在 buffer。

这里还没有真正 mutation 文件。

---

#### Step 3：再发送 control request

全部收到 data 后：

```
Client ---> A

WRITE(dataID, offset)
```

A 是 Primary。

---

#### Step 4：Primary assign order

假设同时还有其他请求：

```
W1
W2
W3
```

Primary：

```
W1 -> 100
W3 -> 101
W2 -> 102
```

---

#### Step 5：Primary apply

```
A:
apply mutation #100
```

---

#### Step 6：Primary 通知 Secondaries

```
             +----> B apply #100
A ----------|
             +----> C apply #100
```

所有 Secondary 按 Primary 指定的 serial order apply。

---

#### Step 7：ACK

```
B ---> A
C ---> A
```

然后：

```
A ---> Client
```

论文就是通过 lease holder 对 chunk 内 mutations 分配统一顺序，然后让所有 replicas 按这个顺序 apply。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 11：为什么 Data Flow 和 Control Flow 要分开？

很多人第一次看到会问：

> Client 为什么不直接把 data 发 Primary，然后 Primary 广播？

例如：

```
              --> B
Client -> A
              --> C
```

那么 A 的 NIC：

```
receive data
+
send B
+
send C
```

会成为 bottleneck。

GFS 做：

```
Data:

Client -> A -> B -> C
```

pipeline：

```
Client:
[1][2][3][4]

A:
   [1][2][3][4]

B:
      [1][2][3][4]

C:
         [1][2][3][4]
```

而 control：

```
Client -> Primary
Primary -> Secondaries
```

所以：

```
Data plane ordering
≠
Control plane ordering
```

Data 传播不要求经过 Primary。

Primary 的真正职责不是搬数据，而是：

> **sequencing。**

论文专门将 data flow 与 control flow 解耦，并沿网络拓扑选择链式 pipeline 传播数据。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这一点和你熟悉的 control plane / data plane 思维非常接近。

---

# Part 12：时间线看并发 Write

现在两个 client：

```
time →
```

```
Client1: ---- push X ------------------------------->
Client2: ------ push Y ----------------------------->

Primary:          recv write X
                       recv write Y
                   X -> serial 100
                   Y -> serial 101

Secondary B:          apply X
                              apply Y

Secondary C:             apply X
                                  apply Y
```

结果：

```
Primary:
X then Y

B:
X then Y

C:
X then Y
```

真正的 invariant：

> **同一个 chunk 上，参与同一 mutation sequence 的 replicas 按 Primary 指定的相同 order apply mutation。**

如果这个 invariant 被破坏：

```
A: X,Y
B: Y,X
```

那么两个 overlapping write 最终可能产生不同 bytes。

---

# Part 13：如果 write 中途失败呢？

现在开始制造 failure。

```
Client
  |
  v
Primary A
 |
 +--> B success
 |
 +--> C failure
```

结果可能：

```
A = mutation applied
B = mutation applied
C = mutation missing
```

这时候 GFS **不会假装这是成功的 perfectly atomic distributed write**。

Primary 会报告错误。

Client retry。

而且 paper 明确承认：

```
failed mutation
```

可能留下 inconsistent region。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这是理解 GFS 最重要的地方之一：

> **GFS 没有追求 POSIX + Linearizability + distributed transaction 那种最强语义。**

它选择：

```
weaker consistency
+
application-aware API
+
higher scalability / simplicity
```

---

# Part 14：GFS 的 Consistency Model

先搞清楚两个非常容易混淆的词。

### Consistent

论文中的意思近似：

> 不管 Client 从哪个 replica 读取，都看到相同数据。

例如：

```
A = XYZ
B = XYZ
C = XYZ
```

consistent。

---

### Defined

更强。

除了所有人看到一样之外：

> 大家看到的还必须确实是某次 mutation 要写的完整结果。

因此：

```
Defined
   =>
Consistent
```

但是：

```
Consistent
!=>
Defined
```

---

### 一个例子

两个 concurrent writes：

```
Write1 = AAAAAA
Write2 = BBBBBB
```

最终所有 replicas 都得到：

```
AABBBB
```

那么：

```
所有 replicas 一样
=> consistent
```

但：

```
结果既不是 AAAAAA
也不是 BBBBBB
```

所以：

```
undefined
```

论文中：

````
successful serial write
→ defined

concurrent successful write
→ consistent but possibly undefined

failed write
→ potentially inconsistent
``` :chatgpt-content-reference{index="12"}


这里千万不要自动套：

```text
Linearizability
````

GFS 的普通 file-data mutation semantics 并不是“整个文件系统都是 linearizable”。

---

# Part 15：为什么 Google 可以接受这么弱？

因为 workload 是关键。

原 GFS 假设：

```
random overwrite
≈ 少

append
≈ 多

streaming read
≈ 多
```

应用往往：

```
generate data
      ↓
append
      ↓
finish
      ↓
read many times
```

而不是：

```
Database page
不断原地修改
```

论文明确指出 multi-GB file 很常见，许多写入是 large sequential append，random write 并非主要优化目标，同时 sustained bandwidth 比 individual operation latency 更重要。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

所以 GFS 不是：

> “一个更好的 POSIX filesystem。”

而是：

> **一个针对 Google data-processing workload 重新设计的 filesystem。**

---

# Part 16：Record Append——这篇 paper 最重要的 API 创新之一

假设 100 个 worker 同时产生结果：

```
Worker1 --+
Worker2 --+
Worker3 --+--> result-file
...       |
Worker100-+
```

传统做法：

```
offset = file_size()
write(offset, record)
```

两个 client：

```
C1: size() -> 1000
C2: size() -> 1000
```

然后：

```
C1 writes at 1000
C2 writes at 1000
```

冲突。

你可能需要：

```
distributed lock
```

但 GFS 直接提供：

```
RecordAppend(record)
```

Client 不决定 offset。

而是：

> GFS 决定这个 record 放在哪里。

---

### Record Append Happy Path

Client：

```
Append(record="hello")
```

把 data push 到 replicas。

Primary 看当前 chunk：

```
current end = 100MB
```

决定：

```
record offset = 100MB
```

然后通知所有 replicas：

```
write exactly at 100MB
```

因此多个 Client：

```
C1 record A
C2 record B
C3 record C
```

Primary 可以决定：

```
offset 100 = B
offset 120 = A
offset 140 = C
```

Client 不关心顺序，只关心：

> 我的 record 作为一个完整 unit 被 append 进去。

GFS 的 Record Append 保证成功记录以原子 unit 至少写入一次，但 retry 可以导致 duplicate；GFS 甚至允许 padding 和某些 inconsistent regions。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 17：这里出现 At-Least-Once

假设：

```
Client -> Append(record=R)

Primary:
成功写 R

Client:
没有收到 response
```

Client不知道：

```
成功了？
还是失败了？
```

于是 retry：

```
Append(R)
```

最后文件：

```
...
R
...
R
```

所以：

```
Record Append
=
atomic record
+
at least once
```

不是：

```
exactly once
```

应用如果不能接受 duplicate，要自己加：

```
recordID
```

例如：

```
{
  id: "job42-worker7-record999",
  payload: ...
}
```

Reader deduplicate。

这和你之前问的 RPC retry / idempotency 是完全同一类问题：

```
request executed?
      |
network broke
      |
client cannot know
      |
retry
      |
duplicate possible
```

这条 mental model 非常重要。

---

# Part 18：Primary ≠ Raft Leader

这里是 Top 级别误解。

看起来：

```
Primary
      |
      +-> Secondary
      +-> Secondary
```

很像 Raft：

```
Leader
      |
      +-> Follower
      +-> Follower
```

但它们解决的问题不同。

#### GFS Primary

负责一个 chunk：

```
mutation sequencing
```

它不是在跑 distributed consensus。

Master 指定 lease holder。

---

#### Raft Leader

负责 replicated state machine：

```
command log ordering
+
quorum replication
+
term / election
+
leader change safety
```

Raft 要证明：

```
不同 leader 之间
committed history 不冲突
```

而 GFS chunk lease 更像：

```
temporary exclusive sequencing authority
```

所以：

```
GFS Primary
≠
Raft Leader
```

---

# Part 19：Master ≠ Primary

另外一组特别容易混：

```
              GFS Master
                  |
            grants lease
                  |
                  v
Chunk A:       Primary CS1

Chunk B:       Primary CS9

Chunk C:       Primary CS3
```

Master：

```
global metadata / coordination
```

Primary：

```
per-chunk mutation order
```

因此整个 cluster 可以同时存在非常多个 chunk Primary：

```
Chunk 1 -> A
Chunk 2 -> F
Chunk 3 -> B
Chunk 4 -> A
...
```

这样 write sequencing 也被 distributed 出去了。

---

# Part 20：Lease 为什么需要 Timeout？

假设：

```
Master grants lease to A
```

然后网络 partition：

```
Master  X  A
```

Master不能知道：

```
A 死了？

还是网络坏了？

还是 A 很慢？
```

这是经典 Failure Detector 问题。

如果 Master立即：

```
grant B
```

可能变成：

```
A thinks: I'm Primary
B thinks: I'm Primary
```

这就是 split brain。

所以 lease：

```
A authority valid until T
```

Master失去联系后：

```
不能在 lease expiry 前
随便给另一个 replica overlapping lease
```

旧 lease 到期后：

```
A no longer has authority
```

Master 才可以安全 grant 新 lease。论文明确描述了这种做法。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 21：但是 Lease 和 Consensus 又有什么区别？

Lease 本质在回答：

> **一段有限时间内谁有 authority？**

Consensus：

> **多个节点对某个不可逆决定达成一致。**

Lease 往往依赖：

```
timeout
clock assumptions
```

而 Raft 是通过：

```
term
quorum
log rules
election restrictions
```

保护 committed history。

可以粗略记：

```
Lease
=
temporary ownership

Consensus
=
replicated decision
```

---

# Part 22：Stale Replica

又一个很容易被忽略的问题。

假设：

```
A
B
C
```

C crash：

```
C offline
```

期间：

```
mutation 1
mutation 2
mutation 3
```

A/B 都更新。

现在 C 回来：

```
A = version 10
B = version 10
C = version 7
```

如果 Master 把 C 给 Client：

```
Client read C
```

就会读 stale data。

怎么办？

GFS 给 chunk 使用：

```
version number
```

在发新 lease 时推进版本。

没跟上新 version 的 replica：

```
stale
```

Master 不让它参与新的 mutation，也不会正常把它作为 location 返回，并最终 GC。论文就是利用 chunk version detection 解决这类 stale replica。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 23：这其实是在维护一个重要 Invariant

你可以把它记成：

> **任何被认为是 current 的 chunk replica，都必须属于当前 chunk generation/version。**

否则出现：

```
old replica resurrects
```

这种非常经典的 distributed storage bug。

工程实践里，你经常会看到类似东西：

```
generation number
epoch
term
version
fencing token
```

它们虽然机制不同，但解决的问题都有共同模式：

> **阻止旧 authority / 旧 replica 在恢复后重新污染当前 state。**

---

# Part 24：Master State

我们把 Master state 系统化一下。

```
Master
|
+-- namespace
|
+-- file -> chunk handles
|
+-- chunk version
|
+-- replica locations
|
+-- lease holder
|
+-- replication policy
|
+-- operation log
|
+-- checkpoint
```

其中大致分：

```
Authoritative persistent metadata
----------------------------------
namespace
file -> chunk
version / critical metadata changes

Derived runtime information
---------------------------
current chunk locations
heartbeat-derived health
```

Master 的 metadata 放在 memory 里以获得快速 lookup 和便于扫描；critical metadata mutation 则写 operation log。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 25：Operation Log

Master crash 后，如果 metadata 全没了：

```
chunks 虽然还在
```

系统也不知道：

```
/foo/bar 对应哪些 chunk
```

所以 Master mutation：

```
create file
delete
rename
new chunk
...
```

都会进入：

```
Operation Log
```

Mental model：

```
current metadata
=
checkpoint
+
replay(operation log after checkpoint)
```

这和 database recovery 思想完全一样：

```
WAL
+
checkpoint
+
replay
```

论文把 operation log 称为 Master critical metadata 的持久化记录，而且 Master 在 metadata mutation durable 到本地和远端后才把变化视为可对 Client 生效。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 26：Checkpoint 为什么需要？

假设 operation log：

```
10 years of logs
```

Master restart：

```
replay 5 billion entries
```

恢复时间不可接受。

所以：

```
        operation log
             |
             v
     [checkpoint state]
             |
             + future log entries
```

restart：

```
load checkpoint
+
replay tail
```

就是你在 database、Raft snapshot、etcd snapshot 中不断见到的模式：

```
log grows forever
       ↓
recovery too slow
       ↓
materialize state
       ↓
discard/compact old history
```

GFS checkpoint 可以后台构造，恢复只需要最新完整 checkpoint 和之后的 log。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 27：注意——GFS Master Recovery 不是 Raft

原论文中 Master operation log 有 remote replicas。

但 failover architecture 是：

```
Primary Master fails
       |
       v
external monitoring
       |
       v
start Master elsewhere
       |
       v
use replicated log
       |
       v
change canonical DNS alias
```

而不是：

```
nodes elect new leader via Raft
```

论文还描述 Shadow Masters：

```
read-only
slightly lagging
```

用于提升 read availability。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

所以不要倒因为果：

> GFS 论文发表时并没有“直接拿 Raft 做 metadata HA”。

Raft 论文后来才出现。

---

# Part 28：Failure Model

现在正式定义 system model。

### Node Model

主要考虑：

```
crash / restart
disk failure
process failure
machine failure
```

更接近：

```
crash-recovery
```

而不是：

```
Byzantine
```

服务器可能 restart。

Persistent data 可以保留，也可能由于 disk failure/corruption 丢失。

---

### Byzantine？

不考虑 malicious node：

```
Chunkserver 不会故意撒谎
Master 不会恶意伪造 mutation
```

所以不是 Byzantine Fault Tolerance。

---

# Part 29：Network Model

GFS 是工程系统论文，不是 FLP 那种形式化 model。

合理 mental model 是：

```
messages can delay
RPC can fail
connection can break
node can become unreachable
network partition can occur
```

Client 必须能够：

```
timeout
retry
```

GFS 用 TCP 做 data transfer。

所以不用考虑 TCP 层的：

```
packet duplication
packet reordering
```

直接暴露给 application。

但 RPC 层依然存在：

```
reply lost
timeout
retry
```

因此存在 duplicate operation 问题。

---

# Part 30：Timing Model

不能简单把它说成：

```
pure asynchronous consensus system
```

因为 lease 显式依赖时间：

```
lease expiry
```

更准确地说：

> GFS 是 workload-oriented engineering system，没有围绕 formal synchrony model 来证明协议；它依赖 timeout、lease expiration、heartbeat 和 eventual recovery 进行 coordination。

这和 Raft paper 的分析视角不同。

---

# Part 31：Storage Model

Chunkserver：

```
local persistent disk

chunk
+
checksum metadata
```

Master：

```
metadata in memory
+
operation log on disk
+
checkpoint
+
remote log/checkpoint copies
```

---

# Part 32：Replication 到底容忍几个 Failure？

默认：

```
3 replicas
```

但不能简单写：

```
N = 3
f = 1
```

因为那是 consensus 语境下常见公式。

GFS chunk 的 durability 更接近：

```
只要至少一个有效 replica 还存在
data potentially recoverable
```

例如：

```
A dead
B dead
C alive
```

仍然可以重新复制：

```
C -> D
C -> E
```

但是 durability 已经进入危险窗口。

如果：

```
A dead
B dead
C dead
```

并且没有其他 valid copy：

```
chunk lost
```

论文明确指出真正不可恢复的情况是所有 replicas 在系统来得及 re-replicate 前都丢失。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这和：

```
Raft 3 nodes
必须多数派才能继续 commit
```

完全不是同一套可用性规则。

---

# Part 33：Re-replication

Master Heartbeat 发现：

```
Chunk X:

A ✓
B ✓
C ✗

target replication = 3
```

于是：

```
A ------copy------> D
```

得到：

```
A
B
D
```

Master 会优先修复：

```
replica 丢失最严重的 chunk
阻塞 application progress 的 chunk
live files 的 chunk
```

同时 throttle copy traffic，避免 recovery 本身打爆生产流量。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这是一条非常现实的 Infra 原则：

> **Recovery traffic 也是 workload。**

如果没有 throttling：

```
failure
   ↓
massive reconstruction
   ↓
network saturated
   ↓
more timeouts
   ↓
system collapses
```

这就是常见的 recovery storm。

---

# Part 34：Replica Placement

如果三个副本：

```
Rack 1:
A B C
```

Rack switch/power failure：

```
A B C 全没
```

所以 GFS 会跨 rack placement。

目标不是：

```
server diversity
```

而是：

```
failure-domain diversity
```

你可以映射到现代 Cloud：

```
machine
↓
rack
↓
AZ
↓
region
```

真正的问题永远是：

> Replica 是否跨越 correlated failure domain？

论文的 placement/re-replication 明确考虑跨 racks。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 35：Rebalancing

假设加入新 Chunkserver：

```
A: 90%
B: 85%
C: 92%
D: 0%   <- new
```

不能瞬间：

```
大量数据 -> D
```

否则：

```
network spike
disk spike
write hotspot
```

Master background rebalancing：

```
gradually move replicas
```

目标包括：

````
disk utilization
load
placement
``` :chatgpt-content-reference{index="24"}


这和 Kubernetes scheduler / autoscaling 很像的一点是：

```text
desired distribution
      ↓
observe current state
      ↓
incrementally reconcile
````

但注意：

> GFS Master 不是 Kubernetes Controller framework；这里只是 control-loop mental model 类似。

---

# Part 36：Garbage Collection

Naive delete：

```
Master:
send DELETE to A
send DELETE to B
send DELETE to C
```

然后：

```
DELETE to B lost
```

怎么办？

Master 要：

```
remember pending delete
retry
handle B restart
...
```

GFS 选择 lazy GC。

逻辑删除：

```
/foo/bar
   ↓
hidden deleted name
```

之后 background scan：

```
metadata reference gone
      ↓
chunk becomes orphan
      ↓
Master notices
      ↓
Chunkserver deletes
```

论文甚至保留删除对象一段 configurable grace period，原实现示例中为数日，再回收 orphan chunks。这样可以统一处理 lost deletion RPC、partial creation 和 accidental deletion。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

这里体现一个很重要的 distributed design 原则：

> **很多时候 reconciliation 比 imperative cleanup RPC 更可靠。**

---

# Part 37：Checksums——Replication 不等于 Data Integrity

三个 replicas：

```
A
B
C
```

如果 A 磁盘 bit rot：

```
original:
10101100

corrupted:
10100100
```

A 仍然在线。

Heartbeat：

```
A: I'm alive.
```

没有任何帮助。

所以：

```
Availability
≠
Integrity
```

GFS 为 chunk 内部的数据 block 维护 checksum；原论文是 64 KB block 配 32-bit checksum。读取前验证，如果 mismatch：

````
reject replica
read another copy
report Master
repair corrupted replica
``` :chatgpt-content-reference{index="26"}


这是很值得记的一层：

```text
Replication
protects against loss

Checksum
detects corruption

Version
detects stale replicas
````

三者完全不同。

---

# Part 38：Snapshot

GFS 还支持：

```
snapshot(file/tree)
```

如果一个 1 TB dataset：

Naive：

```
copy 1 TB
```

很贵。

所以：

```
copy-on-write
```

最开始：

```
Original ----+
             +--> Chunk X
Snapshot ----+
```

当 Original 第一次修改：

```
Original ------> Chunk X'
Snapshot ------> Chunk X
```

为避免 snapshot 与已有 write lease race，Master 在 snapshot 前先 revoke / 等待相关 lease 过期，再复制 metadata；实际 chunk data 在首次 mutation 时 copy-on-write。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

# Part 39：GFS 最重要的 Invariants

不用背几十条，掌握这些。

#### Invariant 1

```
file namespace / file->chunk authoritative metadata
必须可以从 durable operation log + checkpoint 恢复
```

否则：

```
数据还在
但文件系统逻辑结构丢失
```

---

#### Invariant 2

```
同一 lease epoch 内，
Primary 为 chunk mutations 决定统一顺序，
参与 mutation 的 replicas 按该顺序执行。
```

否则 replica divergence。

---

#### Invariant 3

```
stale replica 不得被当 current replica 使用。
```

靠 version detection。

---

#### Invariant 4

```
同一 chunk 不应同时存在 overlapping valid mutation authorities。
```

靠 lease expiration / Master lease management。

否则 split brain。

---

#### Invariant 5

```
corrupted replica 不应把 silent corruption 当正常 data 返回。
```

靠 checksums。

---

# Part 40：Safety 到底是什么？

因为 GFS consistency 较弱，不能简单写：

> “GFS guarantees linearizability。”

错误。

它的 Safety 是多个具体 properties。

例如 metadata：

```
namespace mutations atomic
```

并由 single Master、namespace locking、operation log ordering 管理。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

Chunk mutations：

```
successful serial mutation
→ replicas see same defined data
```

Record Append：

```
success
→ record exists atomically at least once
```

但：

```
duplicate allowed
padding allowed
```

所以 GFS 的 correctness 要根据 API contract 定义。

这是一个非常重要的分布式系统思想：

> **Correctness 永远是“系统是否满足它承诺的 specification”，而不是“它是不是最强 consistency”。**

---

# Part 41：Liveness

GFS 想保证：

```
某些机器坏了
      ↓
其他 replicas 仍能 serve
      ↓
Master detects failure
      ↓
re-replicate
      ↓
恢复 replication level
```

前提：

```
至少有有效 copy
网络最终恢复
Master 可运行
有足够可用资源
```

如果 network partition：

```
Safety
```

不会因为“急着恢复 availability”就随便产生 overlapping lease。

但：

```
Liveness
```

可能降低：

```
等待 timeout
等待 lease expiration
无法访问 replica
```

经典：

```
Safety
≠
Liveness
```

---

# Part 42：Failure Matrix

|Failure|会发生什么|数据安全|Availability|Mechanism|
|---|---|---|---|---|
|Chunkserver crash|replica 暂时减少|通常安全|通常仍可用|replication + re-replication|
|Chunkserver restart|重新报告 chunks|通常安全|恢复|heartbeat + version|
|Primary crash|mutation 暂停|避免双 Primary|临时下降|lease expiry + new primary|
|Master process crash|restart/replay|metadata 可恢复|暂时下降|operation log + checkpoint|
|Master machine loss|新机器启动 Master|metadata 副本存在时安全|failover delay|replicated log + external monitoring|
|RPC response lost|Client 不知是否执行|取决于 API|retry|idempotency / at-least-once semantics|
|Network partition|部分 server unreachable|lease 防冲突|可能下降|timeout / lease|
|Disk corruption|checksum mismatch|有其他 replica 时可修复|通常可继续|checksum + replication|
|Stale replica returns|可能旧数据|有风险|—|version number|
|Replica count drops|durability risk ↑|暂时降低|通常仍可读|prioritized re-replication|
|所有 replicas 丢失|chunk 永久丢失|❌|❌|replication 无法再救|

---

# Part 43：Top 5 Misconceptions

### ❌ 1. GFS Master 保存所有文件数据

不是。

```
Master = metadata/control
Chunkserver = data
```

Data path 绕过 Master。

---

### ❌ 2. Master 就是 Chunk Primary

不是。

```
Master:
cluster-wide coordinator

Primary:
per-chunk mutation sequencer
```

---

### ❌ 3. 三副本意味着 quorum write

不是。

不要看到：

```
3 replicas
```

就自动想到：

```
2/3 majority
```

原始 GFS mutation protocol 不是 Raft majority commit protocol。

---

### ❌ 4. GFS 是 linearizable filesystem

不是。

GFS 显式选择 relaxed consistency model。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

---

### ❌ 5. Record Append = exactly once

不是。

是：

```
atomic
+
at least once
```

可能 duplicate。

---

# Part 44：再加 5 个工程师特别容易错的直觉

#### ❌ Replication 能发现 bit corruption

不能。

需要 checksum。

---

#### ❌ Heartbeat 没收到就证明节点死了

不能。

可能只是：

```
network delay
partition
overload
```

---

#### ❌ Restart 的 replica 一定可以继续用

不能。

可能 stale，需要 version check。

---

#### ❌ Delete 应该立刻 RPC 删除所有副本

GFS 说明 lazy reconciliation/GC 有时反而更 robust。

---

#### ❌ Strong consistency 永远比 weak consistency 好

不是。

Consistency 是成本—语义 trade-off。

如果 workload 可以利用：

```
append
immutable data
self-validating record
```

没必要为 POSIX-like strongest semantics 支付全部复杂度。

---

# Part 45：为什么这篇 Paper 非常“Google”？

GFS 设计来自几个 workload assumptions。

原论文明确强调：

````
failure is normal

large files

append-heavy workload

streaming reads

high sustained bandwidth

commodity machines
``` :chatgpt-content-reference{index="30"}


所以设计反推：

```text
large files
     ↓
large chunks

append-heavy
     ↓
record append

failure normal
     ↓
replication + checksum + recovery

throughput > latency
     ↓
pipeline / bulk transfer

many chunkservers
     ↓
central metadata + direct data path
````

这就是系统设计里非常重要的方法：

> **不要从技术开始设计，从 workload 和 invariants 开始设计。**

---

# Part 46：GFS 和 Kubernetes Control Plane

这个类比对你的背景比较有帮助，但要注意边界。

GFS：

```
Master
  |
  | desired placement
  v
Chunkservers
```

例如：

```
desired replicas = 3

observed:
A ✓
B ✓
C ✗

Master:
reconcile

create D
```

Kubernetes：

```
Desired State:
replicas=3

Observed:
2 Pods

Controller:
reconcile

create another Pod
```

共同思想：

```
Desired State
      ↓
Observe
      ↓
Compare
      ↓
Reconcile
```

但是差别：

```
Kubernetes:
generic API/controller architecture

GFS:
specialized distributed storage control plane
```

不要把两者说成同一种 protocol。

---

# Part 47：GFS 和 Terraform

Terraform 也有类似：

```
Desired config
      ↓
Observed remote state
      ↓
Diff
      ↓
Apply
```

GFS Master 的 background repair：

```
Desired:
chunk X replicas = 3

Observed:
2

Action:
clone another
```

这是一种：

```
convergence-oriented management
```

但 Terraform 的 consistency/failure semantics 显然不是 GFS 的 replication protocol。

---

# Part 48：GFS 和 etcd

两者差异更重要。

```
etcd
```

优化：

```
small metadata
strong consistency
Raft replicated log
frequent small KV operations
```

GFS：

```
huge data
streaming throughput
large chunks
relaxed data consistency
```

所以非常典型的 architecture 是：

```
Metadata / coordination
        |
       etcd-like
        |
        v
Large data plane
        |
distributed storage
```

甚至现代很多系统：

```
small authoritative metadata
+
huge external object storage
```

都延续这种思路。

---

# Part 49：GFS 和 Kafka

不要说它们“都是 log”就混为一谈。

Kafka：

```
Partition
+
ordered append log
+
consumer offset
```

GFS：

```
general-ish distributed file abstraction
+
chunk storage
```

相似之处：

```
append-friendly
sequential I/O
large throughput
replication
```

但 Kafka 的核心 abstraction 是：

```
ordered record stream
```

GFS 是：

```
filesystem + chunks
```

---

# Part 50：GFS 和 Redis

几乎是 workload 两个极端。

```
Redis
small objects
low latency
memory-oriented
```

```
GFS
huge objects
bulk throughput
disk-oriented
```

这也说明：

> Distributed storage architecture 没有一个 universally optimal design。

---

# Part 51：GFS 和 Distributed Database

Database 一般更在意：

```
transaction
serializability
MVCC
index
point read/write
```

GFS 更在意：

```
bulk storage
streaming
append
failure recovery
```

所以：

```
GFS
≠
Distributed Database
```

但它们共享底层问题：

```
replication
partitioning
failure
ordering
durability
recovery
```

---

# Part 52：GFS 和 Sharding

Chunking 本质是一种 partitioning：

```
File
 |
 + Chunk 0
 + Chunk 1
 + Chunk 2
```

然后：

```
Chunk 0 -> servers A/B/C
Chunk 1 -> servers D/E/F
```

所以：

```
Sharding
=
不同 data partition 分散

Replication
=
同一 partition 保存多个 copy
```

这是必须严格区分的。

例如：

```
Data:
A B C D

sharding:
S1: A B
S2: C D
```

而：

```
replication:
A -> S1,S2,S3
```

不同问题。

---

# Part 53：Paper Problem

作者真正想解决：

> 在 commodity hardware 上构建高 aggregate throughput、可扩展、fault-tolerant 的 distributed filesystem，并针对 Google 自己的大规模 data processing workload 做专门优化。

论文发表时，最大的部署已经达到上千机器、数百 TB 存储，并被数百 clients 并发访问。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf?utm_source=chatgpt.com)

---

# Part 54：Previous Approach

传统 filesystem assumptions 更接近：

```
small-ish files

random updates common

low latency important

machines relatively reliable

POSIX semantics desirable
```

GFS 作者认为这些 assumptions 不匹配他们的 workload。

于是没有机械复制传统 FS。

---

# Part 55：Key Insight

如果只能选一个创新，我不会选：

```
64 MB chunk
```

也不会选：

```
3 replicas
```

我会选：

> **让 workload 改变 abstraction。**

例如：

```
concurrent append 很常见
```

传统答案：

```
让应用自己 distributed lock
```

GFS：

```
那就提供 Record Append。
```

又比如：

```
random overwrite 很少
```

那么没必要为它优化整个系统。

这是 paper 最值得学的系统设计方法。

---

# Part 56：Evaluation

论文做了 microbenchmark，也给了真实 cluster 数据。

两个示例 production/research clusters 分别有数百个 Chunkserver、几十到上百 TB 可用磁盘；论文报告的实际 workload 中 read throughput 明显高于 write throughput，而且 Master 当时处理大约数百 ops/s，并未成为这些 workload 的 bottleneck。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

一个特别好的 failure evaluation 是：

论文杀掉一台存有约 600 GB、约 15,000 chunks 的 Chunkserver，在有 intentional replication throttling 的情况下，受影响 chunks 大约 23 分钟恢复到目标 replication level。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf)

重点不是记：

```
23.2 minutes
```

而是理解：

> **Fault tolerance 的 evaluation 不能只证明“不丢数据”，还应该证明系统能多快恢复冗余。**

因为：

```
failure
↓
replication factor ↓
↓
vulnerability window opens
```

恢复速度决定风险窗口长度。

---

# Part 57：Limitations

GFS 的优点往往也是 limitations 的来源。

### Single Master metadata

简化了：

```
global decisions
placement
namespace
GC
```

但：

```
metadata scalability
availability
```

最终会成为架构上的限制。

---

### Large chunks

对：

```
huge sequential workload
```

很好。

对：

```
lots of tiny files
```

没那么理想。

---

### Relaxed consistency

适合：

```
append-heavy batch data
```

不适合你直接拿它当：

```
transactional database storage semantics
```

---

### Application coupling

GFS 成功的部分原因是：

```
Google controls filesystem
+
Google controls applications
```

所以可以让 application 使用：

```
record IDs
checksums
append semantics
```

共同承担 consistency complexity。

---

# Part 58：What aged well?

很多思想今天仍然非常强。

#### 1. Control/Data plane separation

仍然到处都是：

```
SDN
Cloud storage
Kubernetes
distributed DB
```

---

#### 2. Large data block / extent

现代 distributed storage 仍然很常见。

---

#### 3. Failure as normal operation

现在是基础设施领域默认 mental model。

---

#### 4. Background reconciliation

```
repair
rebalance
GC
scrub
```

仍然是 storage system 核心。

---

#### 5. Workload-aware consistency/API

今天依然是非常高级的 design skill。

---

# Part 59：What changed?

Google 后来用 **Colossus** 作为 GFS 的后继系统。Google 公开资料明确称 Colossus 是 GFS 的 evolution/successor，并指出它用分布式 metadata architecture 来突破 GFS single-master metadata scaling limitations；现代 Colossus 还使用包括 erasure coding 在内的更多 storage techniques。[Google Cloud](https://cloud.google.com/blog/products/gcp/history-of-massive-scale-sorting-experiments-at-google?utm_source=chatgpt.com)

所以演进大致是：

```
GFS

single master metadata
three-way replication
append-oriented

        ↓

Colossus

distributed metadata
much larger scale
multiple encoding / redundancy choices
modern storage workloads
```

但 GFS 的核心思想没有因此“过时”。

反而它像一块 architecture fossil：

> 你可以非常清楚地看到现代 distributed storage 的许多基础思想是怎么形成的。

---

# Part 60：和 HDFS 的联系

如果你看 HDFS：

```
NameNode
   |
DataNodes
```

立刻会感觉非常熟悉：

```
GFS Master
   |
Chunkservers
```

Apache 自己的 HDFS architecture 文档同样强调 commodity hardware、hardware failures 是常态、大数据集和高 throughput。[Apache Hadoop](https://hadoop.apache.org/docs/r2.5.2/hadoop-project-dist/hadoop-hdfs/HdfsDesign.html?utm_source=chatgpt.com)

Mental model 上：

```
GFS Master    ~ HDFS NameNode
Chunkserver   ~ DataNode
Chunk         ~ Block
```

但这是帮助理解，不意味着实现细节完全相同。

---

# Part 61：和你后面要学的 Raft 怎么连接？

GFS 会留下一个很大的问号：

```
          Single Master
               |
        metadata authority
```

那么：

```
Master crash 怎么办？

谁决定新 Master？

两个 Master 怎么避免 split brain？

metadata 怎么强一致复制？
```

GFS 给出了一套当时适合 Google 的 Master log replication + failover 方法。

但课程随后会逐渐把问题抽象出来：

```
不是“GFS Master 怎么 HA”
```

而是：

> **一组机器怎样共同维护一个 fault-tolerant replicated state machine？**

这才进入：

```
Paxos
Raft
Consensus
```

所以 GFS 很像给你制造 motivation：

```
现实系统
   ↓
不断撞到 ordering / authority / failure
   ↓
为什么我们需要更一般的 consensus abstraction
```

---

# Part 62：和 ZooKeeper 的关系

GFS Master 自己承担了很多 coordination：

```
namespace
leases
placement
metadata ordering
```

ZooKeeper 后面会问：

> 能不能提供一个通用 coordination service，让 distributed applications 不用每个系统自己重复造 coordination primitives？

因此：

```
GFS
=
specific storage system

ZooKeeper
=
general coordination service
```

---

# Part 63：和 Spanner 的关系

GFS：

```
large-file storage
weak-ish application-tailored consistency
```

Spanner：

```
distributed database
transactions
externally consistent commits
global scale
```

你可以把课程演进看成：

```
GFS:
怎么把大量 bytes 可靠存下来？

        ↓

Distributed Transactions:
怎么跨 shard 原子修改？

        ↓

Spanner:
怎么把 replication + transactions
+ global ordering 组合起来？
```

---

# Part 64：和 Chain Replication 的关系

GFS data pipeline：

```
Client -> A -> B -> C
```

看起来像 Chain Replication。

但千万不要混淆。

GFS 这里的 chain 主要优化：

```
data transfer
```

mutation ordering 仍由：

```
Primary
```

决定。

Chain Replication：

```
Head -> replica -> replica -> Tail
```

链本身就是 consistency protocol 的核心结构。

所以：

```
GFS pipeline
=
network optimization

Chain Replication chain
=
replication protocol architecture
```

---

# Part 65：这节 Lecture 与 Lab

GFS Lecture 通常没有要求你：

```
实现一个完整 GFS
```

MIT 当前课程安排也把 GFS 作为早期 paper/system lecture，然后进入 KV、Paxos/Raft 等 lab/protocol work。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

但很多 Lab 里你会不断重新遇到今天的问题：

```
RPC timeout
retry
duplicate operation
idempotency
crash
persistent state
ordering
leader authority
stale state
```

特别值得提前建立三个 debugging habit。

---

### Habit 1：任何 RPC 都问

```
request sent
server executed
response lost

then what?
```

---

### Habit 2：任何 replicated state 都问

```
谁决定 order？
```

---

### Habit 3：任何 failover 都问

```
旧 owner 怎么证明已经失去 authority？
```

之后 Raft 的：

```
term
```

fencing token 的：

```
epoch
```

GFS 的：

```
lease/version
```

你会越来越容易放到同一张 mental map 上。

---

# Part 66：完整 Problem → Solution Chain

这是这一课最重要的压缩。

```
数据太大，单机放不下
        ↓
把文件分散到多台机器
        ↓
机器会坏，数据会丢
        ↓
Replication
        ↓
副本在哪？
文件对应哪些数据？
        ↓
Metadata
        ↓
Metadata 太复杂，分布式管理很难
        ↓
Single Master
        ↓
Single Master 会成为 data bottleneck
        ↓
Master only on control path
Client directly talks to Chunkserver
        ↓
文件太大，不能作为单一管理单位
        ↓
Large fixed-size Chunks
        ↓
多个 Client 同时修改 replica
顺序可能不一致
        ↓
Lease + Primary
        ↓
Primary serializes mutations
all replicas follow same order
        ↓
大量数据 replication 会打爆 Primary NIC
        ↓
Separate Data Flow from Control Flow
        ↓
Pipeline data between Chunkservers
        ↓
Concurrent append 需要 distributed synchronization
        ↓
Record Append
        ↓
RPC retry
        ↓
possible duplicate
        ↓
at-least-once semantics
application record IDs
        ↓
Chunkserver crash
        ↓
Heartbeat + Re-replication
        ↓
Restarted replica may be stale
        ↓
Chunk Version
        ↓
Disk may silently corrupt data
        ↓
Checksum
        ↓
Delete RPC may fail
partial garbage may remain
        ↓
Lazy Garbage Collection
        ↓
Master crash
        ↓
Operation Log + Checkpoint + replicas
        ↓
Final GFS Architecture
```

如果你脑子里能长期留下这一条链，这节课已经学到 80%。

---

# Part 67：五级练习

你要求一次性讲完，所以这里不打断教学。建议看到问题先自己想 20 秒，再看后面的答案。

### Level 1 — Concept

Chunk：

```
A B C 三个 replicas
```

为什么 read 可以随便找一个 current replica，而 write 不能随便找一个 replica？

#### 答案

因为 read 不改变 shared state。

Write 需要确保：

```
all replicas apply mutations in compatible order
```

因此需要 sequencing authority。

---

### Level 2 — Execution

Primary：

```
A
```

两个 writes：

```
W1
W2
```

B 先收到 W2 的 data，后收到 W1 的 data。

最终应该按哪个顺序 apply？

#### 答案

Data arrival order 不决定 mutation order。

Primary 指定：

```
serial number
```

假如：

```
W1=10
W2=11
```

B 就按：

```
W1 → W2
```

执行。

这正是：

```
data flow
≠
control/order flow
```

---

### Level 3 — Failure

A 是 Primary。

```
A applied W
B applied W
C did not
```

然后 Primary 告诉 Client operation failed。

Client 能认为 W 完全没发生吗？

#### 答案

不能。

这就是 partial execution。

所以普通 failed mutation 可能产生 inconsistent state，需要 retry/应用语义处理。

---

### Level 4 — Counterexample

如果 Master发现 A unreachable 后立刻让 B 成为 Primary，不等待 A lease expiry，会怎样？

#### 答案

可能：

```
Network partition

A still alive
B newly primary
```

两个 authority 同时存在：

```
split brain
```

它们可能给 mutations 分配不同 order。

---

### Level 5 — System Design

如果让你今天设计：

```
100 TB append-only event storage
```

你需要先问什么？

好的问题不是：

```
用 Raft 还是 Paxos？
```

而应该先问：

```
event 大小？
write pattern？
overwrite 吗？
read pattern？
latency requirement？
throughput requirement？
retention？
failure domain？
consistency requirement？
是否允许 duplicate？
metadata size？
```

然后才决定机制。

这就是 GFS 最重要的设计哲学。

---

# Part 68：30 秒版本

如果面试官问：

> GFS 这篇 paper 讲什么？

可以回答：

> GFS 是 Google 为大规模 data-intensive workload 设计的 Distributed Filesystem。它把大文件切成 64 MB chunks，并在多个 Chunkserver 上复制；Single Master 管理 namespace、chunk mapping、placement 和 leases，但文件数据直接在 Client 与 Chunkserver 之间传输，所以 Master 不在 data path。写入时 Master 给某个 replica lease，使它成为 chunk Primary，由 Primary 统一排序 mutations，所有 replicas 按同一顺序执行。系统通过 replication、versioning、checksums、re-replication、operation log 和 checkpoints 应对频繁故障。同时 GFS 为 append-heavy workload 提供 Record Append，并接受比 POSIX 更弱的 consistency semantics，以换取 simplicity 和 scalability。

---

# Part 69：3 分钟版本

可以组织成：

```
Problem
↓
Architecture
↓
Write protocol
↓
Failure
↓
Trade-off
```

#### Problem

Google 有大量 multi-GB data、streaming workloads、commodity servers，failure 是日常事件。

#### Architecture

```
             Master
         metadata/control
             /   \
            /     \
Client ----        ---- Chunkservers

data:
Client <----------> Chunkservers
```

文件切成：

```
64 MB chunks
```

默认：

```
3 replicas
```

#### Read

Client：

```
file + chunk index
→ Master
→ chunk handle + locations
→ directly read replica
```

#### Write

Master 为 chunk 选择 lease holder：

```
Primary
```

Client 先把 data pipeline 到 replicas，再向 Primary 发送 mutation request。

Primary：

```
serialize mutations
```

所有 Secondary 按同一顺序执行。

#### Failure

```
chunkserver failure
→ re-replication

stale replica
→ version

bit corruption
→ checksum

master failure
→ operation log + checkpoint + replicated metadata log
```

#### Consistency

GFS 不追求完整 POSIX / linearizable semantics。

特别支持：

```
Record Append
```

提供：

```
atomic record
at-least-once
```

允许 duplicate。

#### Key insight

整个系统不是围绕“最强语义”设计，而是围绕：

```
Google workload
```

设计。

---

# Part 70：深入版本

把整节课压缩成你以后做 System Design 时可以直接拿来用的一张表：

|Dimension|GFS|
|---|---|
|Problem|huge distributed storage|
|Workload|large files, streaming read, append-heavy|
|Partitioning|fixed-size chunks|
|Metadata|Single Master|
|Data storage|Chunkservers|
|Data path|Client ↔ Chunkservers|
|Replication|default 3|
|Write coordinator|per-chunk Primary|
|Authority|Master-granted lease|
|Ordering|Primary serial numbers|
|Data propagation|pipelined separately from control|
|Consistency|relaxed|
|Concurrent append|Record Append|
|Retry semantics|duplicates possible|
|Failure detection|heartbeat / RPC failure|
|Stale replicas|version numbers|
|Corruption|checksums|
|Repair|background re-replication|
|Placement|failure-domain aware|
|Deletion|lazy GC|
|Metadata durability|operation log|
|Recovery acceleration|checkpoint|
|Major trade-off|weaker semantics for scale/simplicity|

---

# Part 71：最终知识网络

```
                         Distributed Systems
                                |
              +-----------------+------------------+
              |                                    |
           Storage                            Coordination
              |                                    |
              |                           +--------+--------+
              |                           |                 |
             GFS                       Consensus         ZooKeeper
              |                           |                 |
     +--------+--------+                  |                 |
     |        |        |                Raft                |
     |        |        |                  |                 |
 Chunking Replication Metadata      State Machine      Coordination
     |        |        |             Replication
     |        |        |
     |        |        +---- Single Master
     |        |
     |        +---- Failure Recovery
     |               |
     |               +-- version
     |               +-- checksum
     |               +-- re-replication
     |
     +---- Sharding / Placement

              GFS
               |
        +------+------+
        |             |
     Storage       MapReduce
                      |
                  Computation


GFS Write Path
      |
      +-- Lease
      |
      +-- Primary
      |
      +-- Mutation Ordering
      |
      +-- Replicas


GFS Consistency
      |
      +-- Defined
      |
      +-- Consistent
      |
      +-- Record Append
             |
             +-- Atomic record
             +-- At-least-once
             +-- Duplicate possible
```

---

### 这一课最应该形成的 Mental Model

GFS 最值得带走的不是：

```
chunk = 64 MB
replica = 3
lease = 60 sec
```

这些都只是参数。

真正应该留下的是：

```
1. 先研究 workload

2. 分开 metadata/control plane 和 bulk data plane

3. Sharding 和 Replication 是两个问题

4. Replication 后必须回答：
   谁决定 mutation order？

5. Failover 后必须回答：
   旧 authority 怎么失效？

6. Replica 存在不代表 replica 是 current：
   version

7. Replica current 不代表 data 没坏：
   checksum

8. RPC retry 一定要考虑：
   operation 可能已经执行

9. Recovery 不只是“能恢复”：
   还要控制恢复速度和 recovery traffic

10. Correctness 不等于 strongest consistency：
    correctness = 满足你明确承诺的 contract
```

如果把 GFS 放到你后面学习 Raft 的主线里，我建议尤其记住最后一个问题：

```
GFS:
Primary 给 mutations 排序
        ↓
但是 Primary 为什么有权排序？
        ↓
Master 给 lease

然后继续问：

Master 自己的 authority
谁来保证？
        ↓
多个 Master 怎么避免 split brain？
        ↓
metadata 怎么可靠复制？
        ↓

Consensus
        ↓
Paxos / Raft
```

这正是从 **“一个具体 Distributed System”** 走向 **“通用 Distributed Systems Abstraction”** 的关键一步。[Googleusercontent](https://static.googleusercontent.com/media/research.google.com/en/us/archive/gfs-sosp2003.pdf?utm_source=chatgpt.com)

[Google 原始 GFS 论文](https://research.google.com/archive/gfs.html?utm_source=chatgpt.com)