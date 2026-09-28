CRAQ 全称是 **Chain Replication with Apportioned Queries**。

它可以理解成：

> **在 Chain Replication 基础上，让所有副本都能参与读请求，从而提高读吞吐，同时仍然保持强一致性。**

传统 Chain Replication 通常是：

```
Write:
Client
  |
  v
Head -> Node B -> Node C -> Tail

Read:
                         ^
                         |
                       Client
```

也就是：

- 写：从 `Head` 一路传播到 `Tail`
- 读：只从 `Tail` 读

这样很好推理，因为 write 到达 Tail 后才算 committed，所以 Tail 知道“当前最新的已提交版本”。

但问题是：

```
有 4 个副本
却只有 Tail 处理读
```

所以读吞吐受单个 Tail 限制。

CRAQ 的核心改进是：

```
Client -> Head
Client -> Node B
Client -> Node C
Client -> Tail
```

让任意 replica 都可以读。

难点在于：中间节点可能看到一个**还没 commit 的新版本**。

例如：

```
A(Head) -> B -> C(Tail)

A: x=2
B: x=2
C: x=1
```

此时 `x=2` 还没到 Tail，所以还没 committed。

如果 B 直接返回 `x=2`，就可能把未提交的数据暴露给客户端。

所以 CRAQ 引入两个关键概念：

```
Clean
Dirty
```

比如 B 保存：

```
x:
  V1 = 1   clean
  V2 = 2   dirty
```

含义是：

- `clean`：这个版本已知 committed
- `dirty`：这个版本已经收到，但还不知道是否 committed

于是读请求有两种情况：

```
Read(x)

latest version 是 clean
    ↓
直接本地返回

latest version 是 dirty
    ↓
问 Tail：
“当前 committed version 是哪个？”
    ↓
Tail 回答 V1
    ↓
B 从本地返回 V1
```

注意，B 不需要从 Tail 获取完整数据，只需要问一个很小的 metadata：

```
committed_version = 1
```

因此 CRAQ 的核心思想可以压缩成：

```
Chain Replication
        +
Multiple Versions
        +
Clean / Dirty
        +
Tail Version Query
        ↓
Strong Consistency
+
Scalable Reads
```

你可以把它记成一句话：

> **CRAQ = Chain Replication 的“可扩展读版本”：数据可以从任意副本本地读取，但 Tail 仍然是 commit 状态的权威。**

最值得理解的一点不是 `clean/dirty` 本身，而是这个思想：

> **Replica 上“有数据”，不代表这个数据“可以安全返回”。CRAQ 解决的就是 replica 如何判断一个版本是否 safe to read。**