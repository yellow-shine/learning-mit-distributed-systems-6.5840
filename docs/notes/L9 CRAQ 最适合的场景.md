CRAQ 最适合的场景可以先压缩成一句话：

> **大量读、较少写；同一份数据需要多副本容错；又希望读具有较强一致性，并且希望所有 replica 都能承担读流量。**

CRAQ 原论文就是针对 **read-mostly distributed object storage** 设计的：在保持 strong consistency 的同时，把查询分散到所有 replicas，而不是让所有读都压到 Chain Replication 的 tail。[USENIX](https://www.usenix.org/conference/usenix-09/object-storage-craq-high-throughput-chain-replication-read-mostly-workloads?utm_source=chatgpt.com)

### 1. Read-heavy 的 KV / Object Store

这是最典型场景。

假设一个配置对象：

```
key = /products/iphone-18

value = {
    price: 999,
    inventory: ...
}
```

部署：

```
Head -> B -> C -> Tail
```

请求比例：

```
Read   99%
Write   1%
```

普通 Chain Replication：

```
100 万 GET/s
       |
       v
      Tail
```

前三台机器虽然也有数据，却没有承担 read。

CRAQ：

```
250k GET/s -> Head
250k GET/s -> B
250k GET/s -> C
250k GET/s -> Tail
```

大多数时候对象都是：

```
latest version = clean
```

所以直接 local read。

这就是 CRAQ 最大的 sweet spot。论文也明确定位在 object store / key-value 类场景，并强调 per-object ordering 比全局 ordering 更容易高效实现。[USENIX](https://www.usenix.org/legacy/event/usenix09/tech/full_papers/terrace/terrace.pdf?utm_source=chatgpt.com)

---

### 2. Hot Object：一个 key 被大量读取

例如：

```
feature_flag/payment_v2

config/global-routing

product/12345

user-profile/celebrity
```

这种数据可能：

```
每秒更新 1 次
每秒读取 100,000 次
```

如果强一致 read 全打 Leader/Tail：

```
                 100k reads
                     |
                     v
A -> B -> C -> [Tail]
               bottleneck
```

CRAQ 可以：

```
            Load Balancer
          /      |       \
         v       v        v
        A        B        C
```

让副本数量真正变成：

> **read capacity。**

这也是为什么 CRAQ 的价值不只是 Fault Tolerance：

```
Replication
    ↓
传统目的：
Fault Tolerance

CRAQ：
Fault Tolerance
+
Read Scalability
```

---

### 3. Multi-region / Multi-datacenter Local Read

这是 CRAQ 非常有意思的场景。

假设：

```
US-West        US-East        Europe
   A  ---------- B ------------ C
 Head                          Tail
```

普通 Chain Replication：

```
US-West Client
      |
      | Read
      +-----------------------> Europe Tail

             150ms
```

每次 strong read 都跨洲，很痛苦。

CRAQ：

```
US-West Client
      |
      v
US-West Replica A
```

如果：

```
x = V42 clean
```

直接 local read：

```
1~几 ms
```

只有对象当前 dirty：

```
A:
V42 clean
V43 dirty
```

才需要：

```
A ----------------> Tail
   committed version?
```

而且跨 region 传的只是：

```
version = 42
```

不是完整 object。

CRAQ 论文专门讨论了 geo-replicated、多数据中心部署，把 locality-optimized operations 作为重要设计目标。[USENIX](https://www.usenix.org/legacy/event/usenix09/tech/full_papers/terrace/terrace.pdf?utm_source=chatgpt.com)

---

### 4. 大 Object + 高频读取

这一点很容易被忽略。

假设一个 object 是：

```
10 MB
```

普通 Chain Replication：

```
Reader
   |
   | GET
   v
Tail
   |
   | 10 MB
   v
Client
```

所有 reads 都让 Tail：

```
读磁盘/内存
+
发送 10 MB
```

CRAQ dirty read 时，中间 replica 只需要问：

```
Node B ---------> Tail

"现在 committed version 是多少？"

       <---------

"V100"
```

然后：

```
B 本地读取 V100
```

所以 Tail 返回的是：

```
几个 bytes 的 metadata
```

而不是：

```
10 MB object
```

论文也把这一点作为 CRAQ 相对基础 Chain Replication 的重要性能优势：即使出现 dirty reads，tail 主要处理 version metadata，而不是完整 object response。[USENIX](https://www.usenix.org/legacy/publications/login/2009-10/openpdfs/2009annualtech.pdf?utm_source=chatgpt.com)

---

### 5. 配置 / Metadata / Control-plane 类数据

从你熟悉的基础设施角度，可以想象：

```
service configuration
feature flags
routing metadata
cluster metadata
tenant metadata
```

共同特点：

```
写入不频繁
+
读取极其频繁
+
不希望读到乱七八糟的版本
```

例如：

```
Cloud Control Plane

       Config Store
       /    |    \
      R1    R2    R3

API server
Controller
Scheduler
Agent
...
```

如果有：

```
100 个 controllers
10000 个 agents
```

不断读取配置，而管理员偶尔修改配置，那么从 workload shape 来说：

```
Read >> Write
```

非常符合 CRAQ 想优化的问题。

不过这里要注意：

> 这是帮助建立 mental model 的应用场景，不代表 Kubernetes、etcd 或某个具体 cloud control plane 实际使用 CRAQ。

现代 control-plane metadata 更常见的是 Raft/Paxos + leader/follower read 等机制。

---

## 哪些场景 CRAQ 不太适合？

反过来理解更重要。

### Write-heavy

例如：

```
Read  40%
Write 60%
```

不断：

```
V100 dirty
V101 dirty
V102 dirty
V103 dirty
```

那么很多 read 都需要：

```
replica -> Tail version query
```

CRAQ 的：

```
local clean read
```

优势就明显下降。

---

### 跨很多 key 的复杂事务

比如：

```
Transfer $100:

Account A -= 100
Account B += 100
Ledger += record
```

要求：

```
A
B
Ledger

all-or-nothing
```

这主要是：

```
Distributed Transaction
Serializability
Atomic Commit
```

问题。

CRAQ 的核心模型是：

```
per-object replication
```

不是完整的 distributed transaction protocol。

---

### 需要 Byzantine Fault Tolerance

如果服务器可能：

```
撒谎
篡改数据
恶意返回错误 version
```

CRAQ 不是为这种 failure model 设计的。

它主要考虑 crash/failure，而不是 Byzantine nodes。

---

### 写延迟特别敏感、chain 很长

因为：

```
Head -> A -> B -> C -> D -> Tail
```

write 要一路到 Tail：

```
Write latency
    ↑
chain length
```

所以不能因为：

```
“replica 越多，read 越快”
```

就无限加 replica。

你是在做：

```
Read throughput ↑

vs

Write latency ↑
Replication cost ↑
Recovery complexity ↑
```

的 trade-off。

---

## 最典型的 CRAQ workload

可以记成：

```
                 CRAQ Sweet Spot

                 Read >> Write
                      +
              Strong Consistency
                      +
             Replicated Objects
                      +
               Hot Read Traffic
                      +
            Locality / Multi-region
                      |
                      v

             CRAQ 非常有吸引力
```

一个非常具体的例子：

```
全球配置服务

配置：
payment/routing-policy

更新：
10 次 / minute

读取：
1,000,000 次 / second

Replicas：
Singapore
Tokyo
Oregon
Virginia
Frankfurt
```

你希望同时获得：

```
Replication
→ region/node failure 后仍有数据

Strong Consistency
→ 不随便暴露未 commit 配置

Local Read
→ Tokyo client 尽量读 Tokyo

Read Scalability
→ 5 个 replicas 真正承担 5 份 read traffic
```

这就是非常典型的 CRAQ 型问题。

而且这里最值得你带到后面课程的并不是“以后我要部署 CRAQ”，而是这个更一般的问题：

```
                 Replica 有数据
                       |
                       v
              但它能不能安全地读？
                       |
          +------------+-------------+
          |                          |
        CRAQ                    其他系统
          |                          |
clean / dirty              lease / ReadIndex
tail version               safe timestamp
query                      closed timestamp
```

**CRAQ 是一种答案；“怎样证明 replica 可以安全地提供 read”才是更普遍的 Distributed Systems 问题。** [USENIX](https://www.usenix.org/legacy/event/usenix09/tech/full_papers/terrace/terrace.pdf?utm_source=chatgpt.com)