《**Principles of Computer System Design: An Introduction**》是一本非常值得系统工程师读的书。作者是 MIT 的 **Jerome H. Saltzer** 和 **M. Frans Kaashoek**，2009 年出版，内容来自 MIT **6.033 Computer System Engineering** 课程四十多年积累下来的教学材料。它不是一本“操作系统书”“网络书”或“分布式系统书”，而是试图回答一个更高层的问题：

> **复杂计算机系统应该怎样设计，才能做到正确、可靠、可维护、可扩展？**

MIT 官方对它的定位也正是：研究跨操作系统、网络、数据库、分布式系统、安全、容错和体系结构都反复出现的设计原则与抽象。[MIT OpenCourseWare](https://ocw.mit.edu/courses/res-6-004-principles-of-computer-system-design-an-introduction-spring-2009/?utm_source=chatgpt.com)

## 1. 这本书最大的特点：不是教“系统”，而是教“设计系统”

比如你学习 Raft，通常会问：

```
Leader 怎么选？
日志怎么复制？
commitIndex 怎么推进？
```

学习数据库会问：

```
WAL 是什么？
锁怎么做？
事务如何 commit？
```

但这本书会继续往上一层问：

```
为什么系统需要 replication？

为什么 replication 会引出 consistency？

为什么 crash 会引出 atomicity？

为什么 shared state 会引出 concurrency control？

为什么 naming 本身就是一个系统设计问题？

为什么 cache 会让 consistency 变难？

为什么 abstraction boundary 会决定系统是否可维护？
```

所以它特别适合建立所谓的 **system design mental model**。

很多系统课是在教：

```
A system works like this.
```

这本书更偏向：

```
Why was the system designed this way?
```

---

# 2. 全书其实可以看成四层

官方目录一共 11 章。Part I 是第 1–6 章，Part II 是第 7–11 章；其中 Part II 目前由 MIT OCW 免费公开。[MIT OpenCourseWare](https://ocw.mit.edu/courses/res-6-004-principles-of-computer-system-design-an-introduction-spring-2009/pages/open-textbook/)

|层次|章节|核心问题|
|---|---|---|
|系统结构|1–2|什么是系统？复杂系统怎么组织？|
|Abstraction / Modularity|3–5|Naming、Client/Server、Virtualization|
|Resource / Performance|6–7|性能、网络、分层|
|Correctness|8–11|Fault Tolerance、Atomicity、Consistency、Security|

完整章节是：Chapter 1 **Systems**；Chapter 2 **Elements of Computer System Organization**；Chapter 3 **The Design of Naming Schemes**；Chapter 4 **Enforcing Modularity with Clients and Services**；Chapter 5 **Enforcing Modularity with Virtualization**；Chapter 6 **Performance**；Chapter 7 **The Network as a System and as a System Component**；Chapter 8 **Fault Tolerance: Reliable Systems from Unreliable Components**；Chapter 9 **Atomicity: All-or-nothing and Before-or-after**；Chapter 10 **Consistency**；Chapter 11 **Information Security**。[MIT OpenCourseWare](https://ocw.mit.edu/courses/res-6-004-principles-of-computer-system-design-an-introduction-spring-2009/pages/open-textbook/)

这里面最值得注意的是：

```
Naming
Modularity
Virtualization
Performance
Networking
Fault tolerance
Atomicity
Consistency
Security
```

你会发现，这几乎就是现代大型系统设计的骨架。

---

# 3. Chapter 3 Naming 其实非常重要

刚看到：

> The Design of Naming Schemes

很多工程师可能会觉得：

> “名字有什么好讲一整章的？”

实际上 Naming 是系统设计里非常深的一层。

比如：

```
example.com
10.0.0.3
/users/123
s3://bucket/key
pod-abc-123
service.default.svc.cluster.local
inode 12345
LSN 100
transaction ID
```

这些全部都是 **name**。

一个名字背后隐藏的问题包括：

```
name → object
```

这个映射由谁维护？

```
DNS:
name → IP

filesystem:
pathname → inode

Kubernetes:
Service → Endpoint

database:
primary key → row

virtual memory:
virtual address → physical page
```

一旦你开始这样看系统，就会发现：

> 很多所谓的 distributed system，本质上都是在维护一个分布式 naming / mapping system。

比如 DNS、ZooKeeper、etcd、Consul、Kubernetes API、metadata service。

这是这本书非常典型的思维方式：

**从具体技术里抽出一个更通用的 abstraction。**

---

# 4. Chapter 4：Client / Service 不只是 RPC

这一章讨论：

> Enforcing Modularity with Clients and Services

核心问题其实是：

```
一个巨大系统
怎么拆？
```

例如：

```
Application
     |
     v
Database
     |
     v
Storage
```

为什么不是：

```
一个进程
里面什么都有
```

因为我们想得到：

```
interface
   ↓
boundary
   ↓
modularity
```

Client / Server 不只是网络架构，而是一种：

> **模块边界。**

例如 PostgreSQL：

```
Client
   |
PostgreSQL protocol
   |
DB Server
```

Kubernetes：

```
kubectl
   |
Kubernetes API
   |
API Server
```

AWS：

```
SDK
 |
API
 |
AWS Service
```

这也是为什么 API design 在系统设计里如此重要。

---

# 5. Chapter 5 Virtualization：系统设计里的另一个核心武器

Virtualization 不只是 VMware。

这本书里的 virtualization 应该理解得更广：

```
physical resource
       ↓
virtual abstraction
```

例如：

```
physical memory
      ↓
virtual memory
```

```
disk blocks
    ↓
filesystem
    ↓
file
```

```
physical CPU
    ↓
process/thread
```

```
physical machines
      ↓
VM
      ↓
container
```

再往云计算发展：

```
servers
   ↓
Kubernetes
   ↓
Pod
```

本质都在做：

> **隐藏底层复杂性，提供一个稳定的 abstraction。**

你做平台工程时经常遇到的：

```
AWS / GCP / Azure
        ↓
Terraform Provider
        ↓
统一 Resource Model
```

其实也是 virtualization / abstraction 思维。

---

# 6. Chapter 8 Fault Tolerance：和 6.824 会大量重合

这一章的标题非常准确：

> **Reliable Systems from Unreliable Components**

也就是：

```
不可靠机器
+
不可靠网络
+
不可靠磁盘

        ↓

可靠系统
```

官方目录显示这一章会讨论 fault / failure、可靠性指标、active faults、redundancy，以及如何把 redundancy 用到软件和数据上。[MIT OpenCourseWare](https://ocw.mit.edu/courses/res-6-004-principles-of-computer-system-design-an-introduction-spring-2009/pages/open-textbook/)

这就是整个分布式系统领域的根问题：

```
replication
quorum
retry
failover
redundancy
recovery
```

为什么 Aurora 有：

```
6 copies
write quorum = 4
read quorum = 3
```

为什么 Raft：

```
2f + 1 replicas
majority quorum
```

为什么 RAID：

```
data + redundancy
```

背后都是同一个设计原则：

> 用 redundancy 把 unreliable components 组合成 reliable system。

---

# 7. Chapter 9 Atomicity 是全书非常重要的一章

官方把 Atomicity 分成两个非常漂亮的概念：

````
All-or-nothing atomicity

Before-or-after atomicity
``` :chatgpt-content-reference{index="4"}


这个区分非常重要。

### All-or-nothing

解决 crash：

```text
transaction:

A
B
C
D
````

系统 crash 在 C：

```
A
B
C
CRASH
```

最终不能留下：

```
A B C
```

而应该是：

```
nothing
```

或者：

```
A B C D
```

所以：

```
All-or-nothing
```

对应：

```
WAL
redo
undo
transaction
recovery
```

---

### Before-or-after

解决 concurrency：

```
T1
T2
```

即使两个事务同时执行，也必须看起来像：

```
T1 → T2
```

或者：

```
T2 → T1
```

而不能出现奇怪的混合状态。

也就是：

```
serializability
locking
concurrency control
```

你可以把它记成：

```
Atomicity
│
├── crash
│      ↓
│  all-or-nothing
│
└── concurrency
       ↓
   before-or-after
```

这个分类非常漂亮，比单纯背 ACID 更能帮助理解系统。

---

# 8. Chapter 10 Consistency 跟你现在学的东西尤其相关

官方目录里这一章包括：

````
Constraints and interface consistency
Cache coherence
Geographically separated replicas
Reconciliation
``` :chatgpt-content-reference{index="5"}


也就是说，它不会只讲：

```text
strong consistency
eventual consistency
````

而是在问：

> 多份状态存在时，我们如何定义“这些状态仍然是正确的”？

典型问题：

```
Replica A = 100
Replica B = 90
Replica C = 100
```

谁是对的？

再例如 cache：

```
DB = X=20

cache1 = 10
cache2 = 20
```

系统是否 consistent？

这就自然通向：

```
cache coherence
replication consistency
quorum
reconciliation
```

进一步就会连接到：

```
Linearizability
Sequential Consistency
Causal Consistency
Eventual Consistency
```

所以你最近学的 Raft、Aurora、CRAQ、COPS、Frangipani 等内容，其实都可以放进这本书 Chapter 8–10 的大框架里理解。

---

# 9. 它和 MIT 6.824 最大的区别

我会这样画：

```
Principles of Computer System Design
                 │
                 │ general principles
                 ▼
        ┌─────────────────┐
        │ System Design   │
        └─────────────────┘
           /     |      \
          /      |       \
         v       v        v
       OS       DB      Network
                         |
                         v
                 Distributed Systems
                         |
                         v
                     MIT 6.824
```

6.824 会深入：

```
GFS
Raft
ZooKeeper
Aurora
Spanner
Spark
COPS
```

而这本书会告诉你：

```
为什么有 replication？
为什么有 atomicity？
为什么要 naming？
为什么 abstraction boundary 很重要？
为什么 caching 引出 consistency？
为什么 redundancy 可以提高 reliability？
为什么 layering 有时反而会有问题？
```

所以可以把：

> **6.824 看成案例和机制**

把：

> **Principles of Computer System Design 看成这些案例背后的设计语言。**

---

# 10. 它和 DDIA 又不太一样

《Designing Data-Intensive Applications》更偏：

```
database
distributed data
replication
partitioning
transactions
streams
```

而这本书范围更底层、更广：

```
                System Design
                     |
    ┌────────────────┼────────────────┐
    ↓                ↓                ↓
Naming          Modularity       Virtualization
    ↓                ↓                ↓
Network        Fault tolerance   Performance
    \                |               /
     \               |              /
      └──── Atomicity / Consistency ────┐
                                        ↓
                                     Security
```

因此我会把三者定位成：

```
Principles of Computer System Design
        ↓
系统设计“哲学 + 基础 abstraction”

MIT 6.824
        ↓
分布式系统经典机制 + 论文

DDIA
        ↓
现代数据系统工程实践
```

它们不是互相替代，而是互补。

---

# 11. 这本书一个非常值得学的地方：Design Principles

这本书真正想训练的其实不是记忆：

```
RPC
VM
cache
transaction
replication
```

而是面对一个新系统时不断问：

```
What is the abstraction?

What is the interface?

What state exists?

Who owns the state?

Where is the boundary?

What can fail?

What survives a failure?

What must be atomic?

What consistency property is required?

Where can we trade correctness for performance?

How do we know the design is correct?
```

这就是系统设计真正的核心能力。

也是为什么你最近问到的：

> failure model 是什么？

> proof intuition 是什么？

其实和这本书的思维方式非常契合。

---

# 12. 阅读建议

以你的学习路线，我**不建议从 Chapter 1 开始逐页线性读到 Chapter 11**。

更值得优先读的是：

```
Chapter 1   Systems
     ↓
Chapter 3   Naming
     ↓
Chapter 4   Client / Service
     ↓
Chapter 8   Fault Tolerance
     ↓
Chapter 9   Atomicity
     ↓
Chapter 10  Consistency
     ↓
Chapter 7   Networking
     ↓
Chapter 5   Virtualization
     ↓
Chapter 6   Performance
     ↓
Chapter 11  Security
```

其中 **8 → 9 → 10** 我尤其推荐连续读。

你会得到一条非常清晰的问题链：

```
机器会坏
   ↓
Fault Tolerance
   ↓
需要 replication / redundancy
   ↓
出现 multiple copies
   ↓
Consistency

同时

执行中会 crash
   ↓
partial update
   ↓
Atomicity
   ↓
WAL / transactions

同时

多个请求并发
   ↓
interleaving
   ↓
Before-or-after atomicity
   ↓
locks / concurrency control
```

这三章基本就是数据库 + 分布式系统很多问题的“共同祖先”。

MIT OCW 目前提供了 Part II，也就是 **Chapter 7–11 的完整 PDF，约 826 页**，另外还有习题、Glossary 和 Design Principles 材料。[MIT OpenCourseWare](https://ocw.mit.edu/courses/res-6-004-principles-of-computer-system-design-an-introduction-spring-2009/pages/open-textbook/)  
[MIT OCW：Principles of Computer System Design](https://ocw.mit.edu/courses/res-6-004-principles-of-computer-system-design-an-introduction-spring-2009/?utm_source=chatgpt.com)

如果只用一句话概括这本书：

> **6.824 教你 Raft、GFS、Spanner 这些系统“怎么工作”；这本书更想教你，为什么优秀系统通常会长成这个样子。**