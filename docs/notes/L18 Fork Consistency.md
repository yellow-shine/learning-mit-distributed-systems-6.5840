## MIT 6.824 — Fork Consistency & Certificate Transparency

这节课非常值得学，因为它把 6.824 的 Failure Model 向前推进了一步：

> **前面的课程主要问：机器会挂怎么办？  
> 这一课开始问：机器没挂，但它在故意骗你怎么办？**

SUNDR 的服务器甚至可以完全被攻击者控制；它仍试图保证一个非常特殊的性质：服务器可以让不同用户看到不同世界，但**一旦把世界 fork，就不能再悄无声息地 merge 回去**。SUNDR 把这个性质称为 **Fork Consistency**。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

Certificate Transparency（CT）把几乎相同的思想用在 TLS certificate 世界：不是简单地“相信 CA 和 log”，而是要求 certificate issuance 进入一个**公开、可验证、append-only 的历史**。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

---

# Part 1：这节课到底想解决什么问题？

如果只允许记住一个问题：

> **如果保存“事实历史”的服务器本身是不可信的，我们怎么防止它向不同客户端讲不同版本的历史？**

### 1. 从我们已经熟悉的世界开始

Raft 中：

```
Client
   |
   v
Leader
 /    \
F1    F2
```

我们担心：

```
Leader crash
Follower crash
packet loss
network partition
message delay
```

但是通常假设：

> 节点可能失效，但正常运行的节点不会恶意伪造协议消息。

也就是 **Crash Fault**。

现在换一个服务器：

```
Alice --------\
               \
                Storage Server
               /
Bob ----------/
```

服务器被攻击者完全控制。

Alice 写入：

```
x = 100
```

服务器却告诉 Bob：

```
x = 0
```

甚至：

```
Alice view:

x=0
 ↓
x=100
 ↓
x=200


Bob view:

x=0
 ↓
x=50
 ↓
x=60
```

两人都觉得：

```
“我的历史看起来完全正常。”
```

这就是 **Equivocation**：

> 同一个 authority 向不同参与者给出相互矛盾但各自自洽的事实。

---

### 2. 为什么 digital signature 还不够？

第一个 naive solution 很自然：

```
每个用户对自己的修改签名。
```

Alice：

```
Write(x=100)
signature_A
```

服务器不能伪造：

```
Write(x=999)
signature_A
```

很好。

但服务器根本不需要伪造。

它只需要：

```
给 Alice：
H0 → A1

给 Bob：
H0 → B1
```

其中：

```
A1 是 Alice 真签的
B1 是 Bob 真签的
```

于是：

```
signature verification = 全部通过
```

问题在于：

> **Signature 证明“谁说了这句话”，不自动证明“所有人听到的是同一个故事”。**

这句话是理解整个 Lecture 的第一把钥匙。

---

### 3. 更深的问题：信息可以被隐藏

假设：

```
Alice: Write(x=1)
```

然后 Alice 下线。

之后 Bob 上线。

恶意 server 告诉 Bob：

```
Alice 从来没来过。
```

Bob怎么知道服务器在撒谎？

答案是：

> **不知道。**

因为 Bob 没有任何外部 information source。

这是一个 information-theoretic 层面的问题。

世界 1：

```
Alice 从未写过
```

世界 2：

```
Alice 写过
但 server 删除/隐藏了
```

对 Bob 来说观察结果完全一样。

所以 SUNDR 并不试图保证：

> malicious server 永远不能欺骗 client。

这是不可能做到的。

它退一步保证：

> **服务器可以 fork 世界，但 fork 之后不能无痕重新合并。**

论文把 Fork Consistency 描述为：如果服务器让 A 看不到 B 的操作，那么当双方之后看到彼此的操作时，这种欺骗会暴露；要持续欺骗，它必须继续维持两个不同世界。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

这就是本 Lecture 的中心思想：

```
Prevent all lies       ❌ impossible
        ↓
Make lies irreversible
        ↓
Make equivocation detectable
```

---

# Part 2：它在整个 6.824 知识地图里的位置

先看你已经学过的主线：

```
                 Distributed Systems
                        |
        +---------------+----------------+
        |                                |
   Crash Faults                    Byzantine Behavior
        |                                |
 Replication                             |
        |                           Equivocation
 Consensus                               |
        |                         Fork Consistency
 State Machine                            |
 Replication                       Transparency Log
        |
 Linearizability
```

更详细：

```
RPC / Threads
     ↓
机器之间怎么通信

Primary/Backup
     ↓
一台挂掉还有副本

Raft / Consensus
     ↓
多个节点如何决定唯一历史

Linearizability
     ↓
client 看到的行为像单机

2PC / Transactions
     ↓
多个 participant 如何 atomic commit

Spanner
     ↓
跨 shard / replica 提供强事务语义

--------------------------------

现在改变 Failure Model：

server 不只是 crash
server 可以撒谎
        ↓
equivocation
        ↓
Fork Consistency
        ↓
SUNDR
        ↓
Verifiable Append-only Log
        ↓
Certificate Transparency
```

最重要的是边界。

### Raft vs Fork Consistency

Raft：

```
问题：
honest-but-fallible replicas
如何维护一个唯一 log？
```

Fork Consistency：

```
问题：
如果维护 log 的服务器本身是 malicious，
client 能保证什么？
```

Raft 的目标更强：

```
所有正常客户端
      ↓
一个 global history
```

SUNDR 接受更弱结果：

```
            / Alice history
history ---<
            \ Bob history
```

但是：

```
fork 后不能重新 join
```

---

### Linearizability vs Fork Consistency

Linearizability：

```
所有 operation
必须能放进一个 global total order
```

例如：

```
A: W(x=1) complete
B: R(x)
```

那么：

```
B 不能再看到 x=0
```

Fork Consistency允许 malicious server：

```
Alice:
W(x=1)

Bob:
R(x)=0
```

但从此 Bob 和 Alice 被分叉。

所以：

```
Linearizability:
one reality

Fork Consistency:
possibly multiple realities,
but realities cannot secretly reunite
```

Fork Consistency 不是“另一种稍弱的 stale read”。

它是针对：

> **Byzantine equivocation**

设计的 consistency model。

---

### Fork Consistency vs Byzantine Consensus

这也非常重要。

PBFT 一类 Byzantine Consensus 的目标：

```
N replicas
其中 <= f Byzantine

剩余 replicas
仍然维护一个 global history
```

经典模型要求类似：

```
N >= 3f + 1
```

而 SUNDR 的思路完全不同：

```
只有一个 server 都没关系
甚至整个 server 被攻陷
```

代价：

```
不能保证 single global history
不能保证 availability
只能保证 fork 不会偷偷 heal
```

所以：

```
BFT:
用冗余 + quorum 阻止 fork

SUNDR:
允许 fork，
但用 crypto 让 fork irreversible
```

这是两套不同的设计哲学。

---

# Part 3：6 个核心 Mental Models

### Concept 1：Equivocation

#### 一句话

> **同一个 authority 向不同 participant 提供互相矛盾的信息。**

例如：

```
Server → Alice:
history = A B C

Server → Bob:
history = A D E
```

而：

```
A B C
A D E
```

各自都可能是有效签名的数据。

#### 和 corruption 的区别

普通 corruption：

```
file = X

server 返回 Y

Hash(X) != Hash(Y)
```

很容易检测。

Equivocation：

```
Alice legitimately signed A1
Bob legitimately signed B1

server 把不同真实信息分别展示给两个人
```

单独看两边：

```
都合法。
```

所以更难。

---

## Concept 2：Authenticated History

我们需要的不是：

```
Sign(operation)
```

而更像：

```
Sign(
    operation,
    commitment_to_previous_history
)
```

简单想象成 hash chain：

```
H0

H1 = H(H0 || op1)
H2 = H(H1 || op2)
H3 = H(H2 || op3)
```

然后用户签：

```
Sign_A(H3)
```

意义：

> Alice 不只是声明“我执行了 op3”，而是在声明“我执行 op3 时看到的历史是 H3 所承诺的那一条历史”。

这一步非常重要。

---

## Concept 3：Fork Consistency

核心 mental model：

```
                 A2 → A3 → A4
                /
H0 → H1 → H2 --
                \
                 B2 → B3 → B4
```

允许 malicious server fork。

但是不允许：

```
                 A2 → A3
                /       \
H0 → H1 → H2 --         → C
                \       /
                 B2 → B3
```

论文里的关键性质正是：一旦两个 honest users 签署 incompatible histories，它们之后无法再次看到彼此的操作而不检测到攻击。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

我建议直接把 Fork Consistency 记成：

> **Fork once, fork forever — unless someone detects the lie.**

---

## Concept 4：Fetch 也是安全相关操作

这是一处特别反直觉的设计。

你可能认为：

```
write 改数据
→ 必须记录

read 不改数据
→ 为什么记录？
```

因为 SUNDR 要记录的不只是：

```
“你做了什么”
```

还要记录：

```
“你做这个决定时看到了什么”
```

假设：

```
F11
 ↓
F12
```

server：

```
给 Client1 看 F11
给 Client2 看 F12
```

两人已经被 fork。

Client1 基于 F11 修改得到：

```
F13
```

如果 Client1 的 read 没留下 cryptographic trace，服务器之后可以：

```
F11 → F12 → F13
```

告诉 Client2：

> 看，F13 很正常。

从记录来看没人知道：

```
F13 实际是基于 stale F11 产生的。
```

于是：

```
fork
 ↓
又被 server merge
```

所以 SUNDR 的 straw-man 连 `fetch` 都写进 signed history。MIT 历年的 SUNDR 题也专门考这个细节。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2011/quizzes/q2-07-ans.pdf?utm_source=chatgpt.com)

---

## Concept 5：Cryptographic Commitment

你会在 SUNDR 和 CT 两边反复看到同一个思想：

```
大量状态
    ↓
hash tree
    ↓
一个小 root hash
```

例如：

```
       Root
      /    \
    H01    H23
   / \     / \
 H0  H1  H2  H3
```

Root：

```
R = H(H01 || H23)
```

只要任何 leaf 改变：

```
certificate 2
    ↓
H2 changes
    ↓
H23 changes
    ↓
Root changes
```

所以一个 32-byte 左右的 root 可以成为整个状态的 **commitment**。

---

## Concept 6：Transparency

传统安全思想：

```
Prevent bad thing
```

Transparency 的思想略有不同：

```
Bad thing 也许仍然能发生
       ↓
但必须留下 publicly auditable evidence
       ↓
不能偷偷发生
```

Certificate Transparency：

```
CA mis-issues cert
       ↓
certificate 必须进入公开 log
       ↓
domain owner / monitor 能发现
```

因此 CT **不证明 certificate 是合法的**。

它证明的是：

> certificate issuance 不能隐藏起来。

RFC 对这一点说得很明确：SCT 并不能保证 certificate 没有 mis-issued；logs 本身也不负责判断 mis-issuance，需要 interested parties 去 monitor。[RFC Editor](https://www.rfc-editor.org/info/rfc6962/?utm_source=chatgpt.com)

---

# Part 4：System Model / Assumptions

### SUNDR

|维度|假设|
|---|---|
|Server|可以 Byzantine / 完全 malicious|
|Client|一些用户可以 malicious，但讨论 honest users 的保证|
|Keys|honest user private key 不泄露|
|Hash|collision-resistant|
|Signature|attacker 不能 forge|
|Network|可 delay / block / partition|
|Client state|client 必须记得自己的 previous operation/version|
|Storage|server storage 不可信|
|Timing|Safety 基本不依赖同步时间假设|
|Availability|malicious server 完全可以 DoS|

SUNDR 明确要求 client 记住自己上一次操作；论文实现中就是保存该用户 last operation/version。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

所以它实际上需要一个很小但非常关键的 trusted client-side state：

```
last_seen_version
```

如果服务器可以让你连这个都忘掉：

```
rollback attack
```

会容易很多。

---

### 最大能容忍什么 Failure？

非常有趣：

```
整个 server 都可以 malicious
```

从数据 integrity 的角度仍然有保证。

但是：

```
availability = 0
```

server 完全可以：

```
drop every request
```

crypto 解决不了：

```
server 不回答
```

这是：

```
Integrity ≠ Availability
```

---

### Certificate Transparency

CT 的 adversary 更复杂：

```
CA 可能 compromised
Log 可能 malicious
Network 可能 partition
```

但假设：

```
Hash secure
Log signature secure
Auditors/monitors 最终存在信息交换渠道
```

CT log 使用 Merkle tree，并通过 signed tree root / Signed Tree Head 等结构让客户端验证历史；RFC 6962 还定义了 inclusion proof 和 consistency proof。[IETF Datatracker](https://datatracker.ietf.org/doc/html/rfc6962?utm_source=chatgpt.com)

它不是固定：

```
N nodes tolerate f faults
```

这种 threshold model。

其安全依赖的是：

```
cryptographic evidence
+
independent observation
+
cross-checking
```

---

# Part 5：SUNDR 算法——从最笨的方法开始

SUNDR 论文非常适合按照：

```
correct but absurd
    ↓
optimize
```

去理解。

论文也是这么展开的。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

---

## Phase 1：Straw-man SUNDR

假设操作历史：

```
H0
```

Alice fetch：

```
H0
 ↓
fetch(f)
```

Alice 对：

```
整个 history
```

签名。

然后 Bob modify：

```
H0
 ↓
fetch_A(f)
 ↓
modify_B(f)
```

Bob 同样签的是：

```
自己的 operation
+
之前完整 history
```

服务器存：

```
Op1 → Op2 → Op3 → ...
```

每个客户端执行操作时：

```
1. acquire global lock

2. download entire history

3. verify signatures

4. 确认自己的 previous operation 仍然存在

5. replay history

6. 检查 authorization

7. append new operation

8. sign history

9. upload

10. release lock
```

这正是论文 straw-man protocol。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

---

## Happy Path

```
time →

Alice      Server           Bob
  |          |               |
  | fetch    |               |
  |--------->|               |
  | H0       |               |
  |<---------|               |
  |          |               |
  | sign H1 |               |
  |--------->|               |
  |          |               |
  |          |<----fetch------|
  |          |-----H1-------->|
  |          |               |
  |          |<---sign H2----|
```

最终：

```
H0 → H1(A) → H2(B)
```

---

## Failure：server 隐藏 A 的 operation

现在：

```
H0 → H1(A)
```

但是 Server 给 Bob：

```
H0
```

Bob 会签：

```
H0 → H1'(B)
```

于是：

```
          H1(A)
         /
H0 -----<
         \
          H1'(B)
```

两边 signature 都正确。

SUNDR 没阻止 fork。

这是允许的。

---

## Server 为什么不能 heal？

假设它想构造：

```
H0 → H1(A) → H1'(B)
```

问题来了。

Bob 当初签的是：

```
Sign_B(
    H0 → H1'(B)
)
```

不是：

```
Sign_B(
    H0 → H1(A) → H1'(B)
)
```

因此把 A 插进去：

```
Bob signature invalid
```

反过来也一样。

这就是：

```
history binding
```

---

## 为什么 client 还必须检查自己的 previous operation？

Alice 之前签：

```
H0 → A1
```

下一次服务器给她：

```
H0 → B1
```

如果 Alice 不记得：

```
A1
```

她可能认为：

```
“一切正常”
```

继续签：

```
H0 → B1 → A2
```

服务器就成功让 Alice：

```
rollback
+
switch branch
```

因此 Alice 要检查：

```
my_previous_op ∈ current_history
```

这就形成了一个非常强的 invariant：

> **Client 永远只能继续自己已经承认过的历史。**

---

# Part 6：用 timeline 看真正的 fork

```
time ------------------------------------------------->

Alice:
       read H0
          |
       write A1
          |
       Sign(H0,A1)
          |
          |--------------------->
                                Server


Bob:
                                |
                         server hides A1
                                |
       <------------------------|
       receives H0
          |
       write B1
          |
       Sign(H0,B1)
```

此时：

```
Alice remembers:
H_A = H(H0,A1)

Bob remembers:
H_B = H(H0,B1)
```

现在 server 想把 Bob 带回 Alice branch：

```
H0 → A1 → B1
```

但：

```
signature_B
```

不匹配这个 history。

想让 Bob重新签？

Bob 又会检查：

```
previous B1 是否存在？
```

如果构造：

```
H0 → A1 → B1(old)
```

`B1(old)` 本身对 prefix 的 commitment 又不对。

因此死路。

这就是 **no-join property**。

---

# Part 7：为什么 Straw-man 不实用？

两个致命问题：

```
1. 每次传整个 history
2. 一个 global lock 串行所有人
```

论文也明确指出这两个问题。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

假设：

```
1 billion operations
```

每次 read：

```
download 1 billion entries
```

显然不行。

于是 SUNDR 的下一个问题是：

> 能不能不签“整个 history 内容”，而签一个很小的 commitment？

答案：

```
Hash Tree
+
Version Vector
```

---

# Part 8：Serialized SUNDR

SUNDR 把：

```
完整 history
```

压缩成：

```
当前 state 的 cryptographic commitment
+
我见过每个用户哪个 version
```

论文把每个用户/组的文件状态通过 hash tree 聚合成 **i-handle**，再使用 version vector 把各 principal 的最新状态关联起来。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

可以把复杂的 filesystem 细节暂时压成：

```
Alice files
     ↓
 Merkle-like Hash Tree
     ↓
Alice i-handle
```

例如：

```
        hAlice
       /      \
   file1hash  file2hash
```

所以：

```
hAlice
```

是 Alice 可写数据的 commitment。

---

## Version Structure

Alice 的 signed state 可以理解为：

```
VersionStructure {
    my_state_hash: hAlice

    vector: {
        Alice: 5
        Bob:   3
        Carol: 7
    }
}
signature_A
```

它表达：

> 我现在是 Alice version 5；我在创建这个状态时已经看到 Bob version 3、Carol version 7。

这和你学过的 Vector Clock 非常接近。

---

## 关键公式

论文定义：

\[ x \le y \]

当且仅当：

\[ \forall p,\quad x[p]\le y[p] \]

这里：

```
∀ = for all / 对所有

p = principal
    比如 Alice / Bob / group

x[p] = x 这个 version vector
       对 p 的 version 记录
```

例如：

```
x = [A=2, B=3]
y = [A=3, B=4]
```

那么：

```
2 <= 3
3 <= 4

所以：

x <= y
```

意味着：

> y 至少知道 x 已经知道的所有进展。

---

## Fork 如何表现为 incomparable vectors？

初始：

```
[A=1, B=1]
```

Alice 更新，看到了 B1：

```
Alice:
[A=2, B=1]
```

server 把 Alice2 隐藏给 Bob。

Bob 更新：

```
Bob:
[A=1, B=2]
```

现在：

```
X = [2,1]
Y = [1,2]
```

检查：

```
X <= Y ?

A:
2 <= 1 ❌
```

再反过来：

```
Y <= X ?

B:
2 <= 1 ❌
```

所以：

```
X ⧸≤ Y
Y ⧸≤ X
```

这两个 state 是：

> **incomparable**

这就是 cryptographic fork 的数学表现。

SUNDR 的客户端要求它看到的 version structures 加上自己准备签的新结构，在这个 component-wise order 下形成一个 total ordering；一旦出现上面这种 incompatible vectors，client 就能检测 inconsistency。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

---

## 为什么还是无法 merge？

服务器可能想给 Alice：

```
[A=2,B=2]
```

但为了创建这个状态，它必须同时解释：

```
[A=2,B=1]
[A=1,B=2]
```

两者本身 incomparable。

客户端 consistency check 会发现：

```
X ⧸≤ Y
Y ⧸≤ X
```

因此拒绝。

于是：

```
                 [2,1] → ...
                /
[1,1] ----------
                \
                 [1,2] → ...
```

依然：

```
不能 join
```

这就是 straw-man：

```
signed complete history
```

到实际方案：

```
signed compact summary
```

的核心转换。

---

# Part 9：Concurrent SUNDR 为什么还需要更多机制？

Serialized SUNDR 还有：

```
global lock
```

因此：

```
Alice 修改完全不同的 file
Bob 修改另一个 file

也不能并发
```

论文进一步引入 **update certificates**，让用户预先声明即将执行的 fetch/modify，从而允许更多非冲突操作并发。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

这里最重要的 mental model 不需要陷入细节：

```
Straw-man:
整个 history 是 dependency

Serialized:
version vector 是 dependency summary

Concurrent:
进一步显式描述 operation dependency
允许 independent operations overlap
```

这和数据库里：

```
coarse global serialization
        ↓
dependency/conflict tracking
        ↓
more concurrency
```

是一模一样的工程演进。

---

# Part 10：Certificate Transparency 为什么会出现？

现在把同一个问题换成 TLS。

传统 PKI：

```
Browser
   |
   | trusts
   v
CA
   |
   | signs
   v
Certificate
```

假设攻击者控制 CA，给：

```
google.com
```

偷偷签了一个攻击者公钥：

```
Cert(
 google.com,
 attacker_public_key
)
```

signature 是合法的。

Browser：

```
CA trusted?
✅

signature valid?
✅
```

于是接受。

Certificate Transparency 的历史背景中，DigiNotar 在 2011 年发生过 google.com wildcard certificate 被错误签发并用于 MITM 的事件，这也是 CT 论文开篇使用的 motivating example。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

问题和 SUNDR 已经非常相似：

```
不是：
signature 是否是真的？

而是：
authority 做的事情能不能偷偷藏起来？
```

---

# Part 11：CT 的核心设计

CT 建一个：

> **Public, verifiable, append-only log** [MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

想象：

```
Certificate 1
Certificate 2
Certificate 3
Certificate 4
...
```

任何 certificate issuance 都尽量留下公开证据。

于是攻击者从：

```
偷偷签一个 certificate
```

变成：

```
如果想让客户端接受，
必须留下 observable evidence
```

这就是：

```
Prevention
    ↓
Accountability / Transparency
```

---

# Part 12：为什么不能直接下载整个 CT log？

最 naive：

```
Browser:
download entire log

检查：
old_log 是否是 new_log prefix
```

正确，但是非常昂贵。

SUNDR刚刚遇到过一模一样的问题：

```
完整 history 太大
```

CT 的答案也是：

```
Merkle Tree
```

论文明确从“客户端下载完整 log”这个 naive design，引出 Merkle tree 来降低验证成本。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

---

# Part 13：Merkle Tree

四张证书：

```
C0 C1 C2 C3
```

leaf hashes：

```
h0 = H(C0)
h1 = H(C1)
h2 = H(C2)
h3 = H(C3)
```

继续：

```
h01 = H(h0 || h1)

h23 = H(h2 || h3)

root = H(h01 || h23)
```

图：

```
              Root
             /    \
          h01      h23
         /  \      /  \
       h0   h1   h2   h3
       |    |    |    |
       C0   C1   C2   C3
```

一个 root：

```
root = abc123...
```

commit 整个 certificate set/history。

任何一张 certificate 变化：

```
C2'
 ↓
h2'
 ↓
h23'
 ↓
root'
```

---

# Part 14：Inclusion Proof

问题：

> 怎么证明 C2 在 log 中？

不需要给 client：

```
C0 C1 C3
```

只需要给：

```
h3
h01
```

Client：

```
h2 = H(C2)

h23 = H(h2 || h3)

root' = H(h01 || h23)
```

如果：

```
root' == signed_root
```

则证明 C2 属于这个 tree。

proof size：

\[ O(\log N) \]

假设：

```
N = 1,000,000
```

大致只需约：

```
log2(1,000,000)
≈ 20
```

层 hash。

而不是下载 100 万 certificate。

---

# Part 15：Consistency Proof

Inclusion proof 回答：

```
“C2 是否在这个 tree？”
```

但是还不够。

我们还需要问：

> 今天的 log 是否真的是昨天 log 的 append-only extension？

昨天：

```
C0
C1
```

root：

```
R2
```

今天：

```
C0
C1
C2
C3
```

root：

```
R4
```

Consistency Proof 要证明：

```
Tree(R2)
```

确实是：

```
Tree(R4)
```

的 prefix。

而不是：

```
昨天：
C0 C1

今天偷偷改成：
C0 C9 C2 C3
```

RFC 6962 为 Merkle tree 定义了这种 consistency proof；同一个 log 的两个 Signed Tree Heads 可以用 consistency proof 来判断新 tree 是否是旧 tree 的 append-only extension。[RFC Editor](https://www.rfc-editor.org/info/rfc6962/?utm_source=chatgpt.com)

所以一定要区分：

```
Inclusion Proof
= 某东西在里面


Consistency Proof
= 新历史没有重写旧历史
```

---

# Part 16：Signed Tree Head

如果 server 只告诉你：

```
root = abc
```

它以后可以说：

```
我从来没说过 abc。
```

所以 log 要签 root。

抽象：

```
STH {
    tree_size
    timestamp
    root_hash
}

Signature_Log(...)
```

即：

> **Signed Tree Head**

RFC 6962 的 TreeHeadSignature 确实包含 tree size、timestamp 和 SHA-256 root hash。[IETF Datatracker](https://datatracker.ietf.org/doc/html/rfc6962?utm_source=chatgpt.com)

于是：

```
Log 给 Alice：
STH(size=100, root=A)

Log 给 Bob：
STH(size=100, root=B)
```

且：

```
A != B
```

现在已经不是：

```
Alice says...
Bob says...
```

而是有：

```
LogSignature(root=A)
LogSignature(root=B)
```

这是可以拿出去证明：

> **这个 log equivocated。**

---

# Part 17：但 CT 仍然存在 SUNDR 同样的问题

这是本 Lecture 把 SUNDR 和 CT 放一起的真正原因。

假设 malicious CT log：

```
Victim Alice
     |
     | gets
     v

Certificate:
google.com -> attacker key
```

log 对 Alice 构造世界：

```
CT View A:

...
malicious-cert
...
```

但是对 Google monitor：

```
CT View B:

...
没有 malicious-cert
...
```

两个 tree 都：

```
internally valid
```

两个 root 都：

```
signed
```

如果 Alice 和 monitor 永远不交换 root：

> **没人知道世界已经 fork。**

这就是 SUNDR 的问题原样重现：

```
SUNDR:
不同 users 看不同 filesystem history

CT:
不同 observers 看不同 certificate log history
```

CT 原始文章特别指出，不同参与者必须确认大家看到的是同一个 log，否则 log 可以向 client 显示含恶意 certificate 的 view，同时向被冒充的网站展示不含该 certificate 的 view。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

---

# Part 18：Gossip 为什么重要？

Alice：

```
STH_A
```

Bob：

```
STH_B
```

如果：

```
Alice <---- gossip ----> Bob
```

就可以比较：

```
tree_size
root
signature
```

如果：

```
same size
different root
```

直接抓到 equivocation。

如果：

```
sizeA < sizeB
```

要求：

```
ConsistencyProof(A,B)
```

如果 log 无法给合法 proof：

```
misbehavior detected
```

因此：

```
Merkle tree
    ↓
让 history 有 compact commitment

Signature
    ↓
让 log 无法否认 commitment

Consistency proof
    ↓
验证 append-only evolution

Gossip / cross-check
    ↓
发现 split view
```

原 CT 文章甚至把 gossip 视为其中最困难、当时仍在探索的环节。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

---

# Part 19：SCT 又是什么？

这里容易混：

```
SCT
vs
STH
```

### SCT — Signed Certificate Timestamp

当 CA 提交 certificate：

```
CA
 |
 | certificate
 v
CT Log
```

log 不一定立即把它完全 merge 到 Merkle tree。

它先返回：

```
SCT
```

含义近似：

> **“我承诺在规定时间内把这个 certificate 放进 log。”**

Certificate Transparency 官方介绍称 SCT 是 log 对未来 inclusion 的签名承诺，certificate 需要在 Maximum Merge Delay（MMD）内进入 log。[Certificate Transparency](https://certificate.transparency.dev/howctworks/?utm_source=chatgpt.com)

所以：

```
SCT
= promise of future inclusion

STH
= commitment to current tree
```

不要混起来。

---

## CT Happy Path

```
CA                    CT Log                 Browser
 |                       |                       |
 |--- precertificate --->|                       |
 |                       |                       |
 |<------ SCT -----------|                       |
 |                       |                       |
 | issue certificate     |                       |
 |---------------------------------------------->|
 |                 certificate + SCT             |
 |                       |                       |
 |                  append cert                  |
 |                       |                       |
 |                  Merkle tree                  |
 |                       |                       |
 |                 new signed root               |
```

然后 auditor 可验证：

```
certificate
    ↓
inclusion proof
    ↓
STH
```

Monitor：

```
持续扫描 log
      ↓
发现不属于自己 domain 的 cert
```

---

# Part 20：SUNDR 和 CT 的统一 Mental Model

现在应该能看到它们完全是同一类问题。

### SUNDR

```
Operation history
       ↓
cryptographic commitment
       ↓
signed user state
       ↓
different users compare causal histories
       ↓
fork detectable
```

### CT

```
Certificate history
       ↓
Merkle root
       ↓
Signed Tree Head
       ↓
different observers compare tree heads
       ↓
fork detectable
```

统一起来：

```
              Untrusted Authority
                     |
              maintains history
                     |
                     v
          Cryptographic Commitment
                     |
                 Signature
                     |
            +--------+--------+
            |                 |
         Client A          Client B
            |                 |
          View A            View B
            |                 |
            +---- compare ----+
                     |
               equivocation
                detectable
```

这是本 Lecture 最重要的抽象。

---

# Part 21：State / Invariants

### SUNDR Client State

概念上最重要：

```
private_key
public identities
previous version structure
version vector
i-handle / state commitment
```

尤其：

```
previous version
```

是不能随便丢的。

---

### SUNDR 核心 Invariant 1

```
客户端永远不会接受一个
不包含自己 previous accepted state 的历史。
```

否则 server 可以：

```
rollback client
```

---

### Invariant 2

两个合法的连续状态必须满足：

```
old <= new
```

也就是：

```
所有 component 都只能前进
```

不能：

```
[A=5,B=7]
 ↓
[A=5,B=3]
```

这类似：

```
monotonic knowledge
```

---

### Invariant 3

一旦：

```
X ⧸≤ Y
Y ⧸≤ X
```

两个 branch 就不能重新生成一个被双方无感接受的 common history。

这就是：

```
no-join
```

如果这个 invariant 被破坏：

```
malicious server 可以短暂欺骗
↓
事后修复历史
↓
所有证据消失
```

Fork Consistency 就没有意义了。

---

## CT Invariants

CT 最重要：

```
1. Existing log entries cannot change.

2. Existing entries cannot disappear.

3. New states must extend old states.

4. Signed roots bind log to a view.

5. A claimed included certificate
   must have a valid inclusion path.
```

注意：

```
CT 并不能仅靠一个 isolated client
保证不存在另外一个 fork。
```

你仍然需要：

```
cross-observation
```

---

# Part 22：Correctness

### SUNDR Safety

SUNDR 的核心 Safety 不是：

```
server 永远不会撒谎
```

而是：

> malicious server 无法伪造 honest users 的授权修改；如果它通过隐藏操作制造 incompatible client views，那么这些 fork 不能在不暴露 inconsistency 的情况下重新合并。

论文的证明依赖 digital signatures 和 collision-resistant hash function。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

Mechanism：

```
signatures
   ↓
server 不能 forge writes

hash commitments
   ↓
server 不能 silent mutate state

client previous-state memory
   ↓
不能 rollback branch

version vectors
   ↓
检测 incompatible histories
```

---

### SUNDR Liveness

非常弱。

恶意 server：

```
drop requests
```

系统就停了。

所以：

```
Safety  ✅
Liveness ❌ against malicious server
```

这恰好再次说明：

```
Safety ≠ Liveness
```

---

## CT Safety

假设 cryptography 没被破坏：

```
Merkle inclusion proof
→ 不能假装某 leaf 属于某 root

Consistency proof
→ 不能假装 rewritten tree 是 old tree extension

Signed Tree Head
→ log 不能否认自己说过什么
```

但：

```
Split view
```

仍然可以持续一段时间。

必须：

```
STH_A meet STH_B
```

攻击才暴露。

RFC 6962 同样将 presenting conflicting tree views 视为 misbehaving log，并提出通过比较 Signed Tree Heads / consistency proof 来检测。[RFC Editor](https://www.rfc-editor.org/info/rfc6962/?utm_source=chatgpt.com)

---

# Part 23：Failure Matrix

### SUNDR

|Failure|结果|Integrity|Availability|
|---|---|---|---|
|Server crash|无服务|✅|❌|
|Packet loss|retry / stall|✅|下降|
|Network partition|clients 可能看到不同进度|✅/可能 fork|下降|
|Server deletes data|无法 forge，但可拒绝服务|✅|❌|
|Server returns stale state|client history checks 限制 rollback|✅|可能失败|
|Server hides another user op|可以制造 fork|Fork guarantee|✅ 可能继续|
|Server attempts merge|cryptographic/history check 失败|✅|branch 继续|
|User key compromised|attacker 可合法代表该用户|❌ 对该权限域|视情况|
|Hash/signature broken|security assumption 崩溃|❌|—|

---

### CT

|Failure|结果|
|---|---|
|CA misissues cert|cert 可被 log 暴露给 monitor|
|Log omits cert after SCT|违反 inclusion/MMD promise|
|Log rewrites old history|consistency proof 失败|
|Log gives two STHs|一旦 views 被比较即可提供 equivocation evidence|
|Network isolates victim|split view 检测可能延后|
|Monitor offline|misissue 可能暂时无人发现|
|Log refuses requests|Availability failure|
|Crypto broken|验证模型失效|

---

# Part 24：Top 5 Misconceptions

### ❌ 1. “有 digital signature 就不会被骗”

错。

Signature：

```
证明：
Alice 确实签过这个东西
```

不证明：

```
Alice 和 Bob 看的是同一个世界
```

这是整节课最大的 lesson。

---

### ❌ 2. Fork Consistency 防止 fork

名字很容易误导。

恰恰相反：

```
Fork Consistency
允许 fork
```

保证的是：

```
fork cannot silently rejoin
```

---

### ❌ 3. Merkle Tree 本身就能阻止 split-view

错。

Server 完全可以维护：

```
MerkleTree_A
MerkleTree_B
```

各自：

```
hash valid
```

真正关键的是：

```
signed roots
+
cross-client comparison
```

---

### ❌ 4. Certificate Transparency 证明 certificate 是合法的

错。

CT 可以记录：

```
一个完全错误的 certificate
```

它仍然可能是一个：

```
valid CT entry
```

CT解决的是：

```
visibility / accountability
```

不是：

```
authorization correctness
```

---

### ❌ 5. CT = Consensus

完全不是。

不同 CT logs 不需要：

```
对所有 certificate 建立唯一 global order
```

每个 log 可以有自己的 append-only history。

CT 主要要求：

```
each log itself
is verifiable and non-equivocating
```

而 Raft：

```
replicas
必须 agreement on one ordered state machine log
```

---

# Part 25：和 Raft 重新对比一次

这是很好的面试级 mental model。

```
                   Raft                 SUNDR / CT

Fault       Crash / partition           Byzantine / equivocation

Core tool   Quorum                      Cryptography

Goal        One history                 Detect inconsistent histories

Fork        Prevent committed fork      Fork may happen

Merge       Not relevant                Fork must not silently merge

Trust       Majority follows protocol   Server may be malicious

Availability majority required          malicious server can DoS

Evidence    replicated logs             signatures / hash commitments
```

还有一个非常漂亮的区别：

```
Raft：
Prevent equivocation from becoming committed state

SUNDR/CT：
Make equivocation leave evidence
```

---

# Part 26：与 Kubernetes / etcd 的联系

你熟悉的：

```
Kubernetes API Server
         |
         v
        etcd
         |
       Raft
```

Raft解决：

```
etcd node crash
network partition
leader failure
```

并让：

```
healthy majority
```

维护唯一 committed history。

但是假设：

```
整个 etcd control plane
+
所有 credentials
+
administrator
```

都恶意了呢？

Raft无能为力。

三个 malicious Raft replicas 完全可以一起撒谎。

这不是 Raft 的 threat model。

---

### 如果 Cloud Control Plane 是 untrusted 呢？

假设 SaaS control plane 告诉 Customer A：

```
Project configuration:
Private networking enabled
```

后台却实际改过 IAM / endpoint policy。

普通 audit log 如果也由同一个 control plane 控制：

```
attacker:
修改资源
↓
修改 audit log
```

那 audit log 没意义。

Transparency 的设计思想会是：

```
control-plane action
       ↓
append-only log
       ↓
Merkle commitment
       ↓
signed checkpoint
       ↓
independent auditor
```

这里不是说 Kubernetes 本身这样实现。

而是：

> **如果 audit authority 与被审计 authority 是同一个完全可信主体，你并没有真正解决 malicious-authority threat。**

---

# Part 27：Terraform State 类比

这是一个非常适合你的例子。

假设 Remote Terraform backend malicious。

Alice：

```
terraform apply

state version 10
 ↓
version 11
```

Bob却被展示：

```
version 10
```

Bob执行：

```
terraform apply
```

生成：

```
version 11'
```

现在：

```
                 state11(Alice)
                /
state10 --------
                \
                 state11'(Bob)
```

普通 locking 解决的是：

```
concurrent writer
```

但如果：

```
lock server 本身 malicious
```

它完全可以同时告诉两人：

```
“你拥有 lock。”
```

所以：

> **Distributed Lock 不能解决 malicious lock authority。**

Fork Consistency 的解决方式不是更相信 lock。

而是：

```
client cryptographically binds
each new state to prior observations
```

这就是它和普通 concurrency control 最深的差异。

---

# Part 28：和 Kafka 的联系

Kafka：

```
partition
 ↓
ordered log
```

看起来很像 CT log。

但意义不同。

Kafka通常假设：

```
broker infrastructure 是可信 service
```

它主要解决：

```
replication
durability
ordering
availability
```

CT log 的问题：

```
log operator itself may lie
```

所以 CT 额外需要：

```
cryptographic commitment
public auditability
cross-observer verification
```

因此：

```
append-only data structure
```

和：

```
cryptographically verifiable append-only log
```

不是一回事。

---

# Part 29：SUNDR Paper 应该怎么读？

### Paper Problem

> 如何把 filesystem 放在完全不可信的 server 上，同时仍然保护 integrity 和 consistency？

SUNDR 明确把 storage server 当作不可信，并用用户公钥控制写权限。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

---

### Previous Approach

一种办法：

```
把 server 防得非常严
```

另一个办法：

```
Byzantine replicated servers
```

SUNDR选择：

```
不要求 server trustworthy
不要求多个 server quorum
依赖 end-client cryptographic verification
```

---

### Key Insight

最关键的思想：

> **无法阻止 malicious server conceal information，但可以保证它一旦对不同用户维护不同历史，就必须永远维持这个 fork，直到攻击被发现。**

论文甚至指出，在没有 online trusted party 的条件下，这种 fork-consistency 是非常强的可实现 integrity guarantee。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

---

### Design Evolution

```
Full signed history
       ↓
太贵

Hash tree / i-handle
       +
Version vector
       ↓
Serialized SUNDR
       ↓
仍然 global lock

Update certificate
       ↓
Concurrent SUNDR
```

非常典型的 systems paper 演化。

---

### Evaluation

SUNDR 原论文实现了实际 filesystem，并报告软件开发 workload 和 microbenchmarks 下总体性能与 NFS 可比较，有些场景更快、有些更慢。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

真正重要的不是 benchmark 数字。

而是证明：

> 这种 cryptographic consistency 并非只能停留在理论模型。

---

### Limitations

最重要几个：

```
1. malicious server 可以 DoS

2. client 必须安全保存自己的 key/state

3. fork 可能长期存在，
   如果 clients 永不交换信息

4. cryptographic overhead / metadata complexity

5. compromised user key
   会失去该权限范围的安全性
```

---

### What aged well?

非常好：

```
Authenticated Data Structures
Merkle Trees
Signed checkpoints
Fork detection
Transparency
Client-side verification
```

---

### What changed?

SUNDR 的具体 filesystem architecture 并没有成为今天主流分布式文件系统模板。

而且论文中的：

```
SHA-1
```

是历史设计；现代 cryptographic systems 已经不会把 SHA-1 当成这种 collision-resistance security foundation。CT v1 RFC 中的 tree root 则明确使用 SHA-256。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

但 SUNDR 背后的：

```
“don't trust; verify history”
```

非常长寿。

---

# Part 30：Certificate Transparency Reading

### Problem

Traditional PKI：

```
CA = trusted authority
```

如果 CA compromised：

```
secret misissuance
```

很难被 domain owner 发现。

---

### Key Insight

不要试图让：

```
CA 永远不犯错
```

而是让：

```
CA 的 issuance observable
```

所以：

```
public
verifiable
append-only
```

成为三个关键词。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

---

### Mechanisms

```
certificate
     ↓
SCT
     ↓
Merkle Log
     ↓
Signed Tree Head
     ↓
Inclusion Proof
     ↓
Consistency Proof
     ↓
Monitor / Auditor
     ↓
Cross-view checking
```

---

### Trade-off

一个非常典型的 systems trade-off：

```
Certificate issuance latency
vs
Immediate global consistency
```

如果 CA 必须等 certificate：

```
同步进入全球所有 log replica
```

再签：

```
issuance latency
```

会非常差。

所以：

```
SCT
```

把它改成：

```
现在给 promise
之后在 MMD 内真正 merge
```

原 CT 文章正是以 availability/consistency trade-off 来解释这个设计。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/ct.pdf)

这其实非常 Distributed Systems。

不是单纯 cryptography。

---

# Part 31：一个特别重要的统一公式

可以把整节课压缩成：

\[ \text{Local Validity} \neq \text{Global Consistency} \]

例如：

```
Alice's history
```

所有 signature：

```
valid
```

Bob 的也：

```
valid
```

但：

```
AliceHistory ≠ BobHistory
```

所以：

```
valid signature
```

只能给你：

```
Authenticity
```

不能自动给：

```
Consistency
```

而这节课研究的是：

> 怎样从 **cryptographic authenticity** 向 **cross-client consistency/accountability** 前进一步。

---

# Part 32：Problem → Solution Chain

整个 Lecture 可以这样记：

```
Untrusted server
      ↓
server 可以篡改数据

Digital Signature
      ↓
不能伪造 unauthorized operation

但：
server 可以隐藏合法 operations
      ↓
different clients see different histories

Sign operation + prior history
      ↓
历史被绑定

但：
完整 history 太大

Hash Tree
      +
Version Vector
      ↓
compact authenticated state

结果：
fork 可以发生
但 fork 无法无痕 merge
      ↓
Fork Consistency
```

再把它推广到 CT：

```
CA can misissue certificate
        ↓
普通 signature 没用
因为 CA 自己就是 signer
        ↓
Public Certificate Log
        ↓
但 log 本身也可能撒谎
        ↓
Merkle Root
        ↓
Signed Tree Head
        ↓
Consistency Proof
        ↓
但 log 可给不同 client 不同 root
        ↓
Gossip / cross-checking
        ↓
split-view becomes detectable
```

这就是整节课。

---

# Part 33：五级理解测试

建议现在先自己推一遍。

#### Level 1

Alice 和 Bob 收到两个不同历史，但两个历史里的所有 operation signature 都合法。

是否已经证明 server honest？

**答案：没有。**

因为 server 可以 equivocate。

---

#### Level 2

历史：

```
H0
 |
 +---- A1
 |
 +---- B1
```

A1：

```
Sign_A(H0,A1)
```

B1：

```
Sign_B(H0,B1)
```

Server 能不能直接构造：

```
H0 → A1 → B1
```

？

**不能。**

因为：

```
B1 signature
```

没有承诺：

```
H0→A1
```

这个 prefix。

---

#### Level 3

如果 Alice 丢失自己的：

```
previous_version
```

可能发生什么？

Server 可以：

```
把 Alice rollback
↓
换到另一个 branch
```

从而破坏 fork guarantee。

---

#### Level 4

假设 CT log 为 victim 构造一个 malicious certificate tree，同时给 monitor 另一个 clean tree。

Merkle tree 会自动发现吗？

**不会。**

两个 tree 都可以 cryptographically valid。

只有：

```
两边的 STH 相遇
```

才发现 equivocation。

---

#### Level 5 — System Design

你设计一个：

```
Multi-cloud control-plane audit log
```

其中 control plane operator 也可能 malicious。

只用：

```
PostgreSQL audit table
```

够吗？

**不够。**

因为 operator：

```
UPDATE audit_log ...
DELETE ...
```

可能同时修改行为和证据。

至少需要类似：

```
append-only commitment
+
signed checkpoints
+
independent observers
```

如果 threat model 真包含 operator compromise。

---

# Part 34：30 秒版本

面试官问：

> Fork Consistency / Certificate Transparency 在讲什么？

你可以这样答：

> Fork Consistency 处理的是 untrusted server 可能对不同客户端 equivocate 的问题。仅靠 digital signature 只能证明单个 operation 的 authenticity，不能保证不同客户端看到相同历史。SUNDR 让客户端把新操作 cryptographically bind 到自己之前看到的 history，因此 server 虽然可以把不同用户 fork 到不同历史，但一旦 fork 就不能在不被发现的情况下重新 merge。Certificate Transparency 把类似思想应用到 TLS certificates：用 Merkle-tree-backed append-only logs、signed tree heads、inclusion/consistency proofs 以及 cross-observer checking，让 certificate issuance 和 log equivocation 可审计。

---

# Part 35：3 分钟版本

主线是：

```
传统 distributed systems:
机器可能 crash

这一课：
server 可能 Byzantine
```

Digital signature 能阻止 server：

```
forge Alice operation
```

但不能阻止：

```
向 Alice 隐藏 Bob
向 Bob 隐藏 Alice
```

所以 SUNDR 要记录客户端：

```
operation
+
previous observed history
```

形成 authenticated history。

如果 server fork：

```
           A branch
          /
history --
          \
           B branch
```

两边客户端会签下 incompatible histories。

因为客户端以后必须延续自己已经签过的 history，所以 server 无法重新 merge 两个 branch。

这就是：

```
Fork Consistency
```

完整 history 太贵，于是 SUNDR 使用：

```
hash tree
+
i-handle
+
version vector
```

压缩 history commitment。

Certificate Transparency面对类似问题：

```
CA 可以 misissue certificate
```

于是把 issued certificates 放入：

```
public verifiable append-only Merkle log
```

通过：

```
SCT
STH
inclusion proof
consistency proof
```

验证。

但是 malicious log 仍然可能：

```
split view
```

因此 observers 必须交换/比较 tree heads。

所以整节课的核心就是：

> **Cryptography 不一定阻止恶意 authority 撒谎，但可以让不同谎言之间产生无法消除的 cryptographic inconsistency。**

---

# Part 36：深入版本

```
Problem
   ↓
Untrusted authority can equivocate

Model
   ↓
Byzantine server
honest clients retain state
secure signatures + collision-resistant hashes

Naive
   ↓
Sign each operation

Failure
   ↓
Valid signed operations can still be selectively hidden

Mechanism
   ↓
Bind operation to observed history

Invariant
   ↓
Client history only moves forward

Failure
   ↓
History too large

Mechanism
   ↓
Hash tree + version vectors

Safety
   ↓
Unauthorized modification cannot be forged
Fork cannot silently rejoin

Liveness
   ↓
Not guaranteed against malicious server

Generalization
   ↓
Certificate Transparency

Mechanism
   ↓
Merkle append-only log
SCT
STH
Inclusion Proof
Consistency Proof
Cross-observer checking

Trade-off
   ↓
Cannot prevent all malicious activity
but turns hidden attacks into auditable evidence
```

---

# Part 37：最终知识网络

把它挂到你的 Distributed Systems Mental Model 上：

```
                         Distributed Systems
                                  |
             +--------------------+--------------------+
             |                                         |
        Fault Tolerance                             Security
             |                                         |
      Crash Fault Model                         Byzantine Model
             |                                         |
        Replication                               Equivocation
             |                                         |
         Consensus                             Authenticated History
        /         \                                    |
     Raft        Paxos                          Fork Consistency
       |                                                |
 State Machine                                        SUNDR
 Replication                                            |
       |                                                |
 Linearizability                             Verifiable Append-only Log
                                                        |
                                                  Merkle Tree
                                                        |
                                  +---------------------+----------------+
                                  |                                      |
                           Inclusion Proof                       Consistency Proof
                                  |                                      |
                                  +-----------------+--------------------+
                                                    |
                                              Signed Tree Head
                                                    |
                                                Cross-check
                                                    |
                                      Certificate Transparency
```

### 最后真正值得记住的四句话

```
1. Signature 证明“谁说的”，
   不证明“所有人听到的是同一个故事”。

2. Fork Consistency 不阻止 fork；
   它保证 fork 不能偷偷 heal。

3. Merkle Tree 解决的是 compact verification，
   不是自动解决 split-view。

4. Transparency 的目标不是让坏事绝对无法发生，
   而是让坏事无法悄无声息地发生。
```

其中第 **1 + 2** 句是 SUNDR 的灵魂，第 **3 + 4** 句是 Certificate Transparency 的灵魂。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf)

原始阅读材料可以直接看 MIT 提供的 [SUNDR paper](https://pdos.csail.mit.edu/archive/6.824-2012/papers/li-sundr.pdf?utm_source=chatgpt.com) 和 [Certificate Transparency reading](https://pdos.csail.mit.edu/6.824/papers/ct.pdf?utm_source=chatgpt.com)；CT 论文时代使用的 RFC 6962 后来已被 RFC 9162 取代，但 Lecture 要建立的 **authenticated append-only history + split-view detection** mental model 仍然是核心。[RFC Editor](https://www.rfc-editor.org/info/rfc6962/)