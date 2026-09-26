# 17 — Cheat Sheet

考前十分钟。数字若标了“论文”，就不是 Beazley 在课上给的常数。

## States

```text
Follower   听心跳。超时则竞选
Candidate  term+1，投自己，要票。多数则当选，否则再超时
Leader     只追加日志。给每个 follower 发 AppendEntries。更高 term 则下台
```

任何角色，见到更大的 term：改 term，变 Follower。这包括响应，不只是请求。

## Terms

```text
每次开始选举 +1。失败不退回。
日志条目的 term = 写入它的那任 leader 的 currentTerm。
可以小于写入者现在的 currentTerm。Figure 8 就建立在这个差别上。
旧 term 的 RPC：拒绝，回复里带上自己的 term。
```

## RPCs

| RPC | 谁发 | 谁收 | 干什么 |
| --- | --- | --- | --- |
| RequestVote | Candidate | 所有人 | term, candidateId, lastLogIndex, lastLogTerm |
| RequestVoteResp | 投票者 | Candidate | term, voteGranted |
| AppendEntries | Leader | 每个 follower，分别组包 | term, leaderId, prevLogIndex, prevLogTerm, entries[], leaderCommit |
| AppendEntriesResp | Follower | Leader | 论文只有 term, success。Visualizer 还有 matchIndex |

空 entries = 心跳。仍检查 prev，仍携带 leaderCommit。

投赞成：本 term 还没投给别人，且候选人日志至少一样新。先比 lastLogTerm，相同再比 lastLogIndex。

## Persistent

论文要求，课上没实现：

```text
currentTerm
votedFor
log[]
```

先落盘，再回复。否则崩溃会投两次，或丢掉已确认的条目。

## Volatile

```text
commitIndex, lastApplied          所有人
nextIndex[], matchIndex[]         只有 leader。当选时重建
```

```text
nextIndex[i]  = 下一次发给 i 的 index
matchIndex[i] = 已知 i 已有的最高 index
prevLogIndex  = nextIndex - 1
```

当选：`nextIndex = lastLogIndex+1`，`matchIndex = 0`。不要继承前任的表。

## Election

```text
安静超过选举超时 → Candidate
随机超时，避免同时醒
自己的一票算数
3 台要 2，5 台要 3
两个多数派必相交
当选后立刻 AppendEntries
听到当前 leader 就重置选举计时
两台永远选不出。这是对的
```

论文常见数量级：选举 150–300ms，心跳显著更短。课堂没给这个数。

## AppendEntries

```text
term 旧 → false
prev 不存在或 term 不同 → false
同 index 不同 term → 删这条及之后，再接上
同 index 同 term → 保持。迟到的包不能误删后缀
然后按 leaderCommit 抬 commit，但不超过自己最后一条
失败：nextIndex--，不要减过左端。课上不写冲突索引优化
成功：matchIndex = prev + len(entries)；nextIndex = matchIndex + 1
```

Leader 不删自己的日志。

## Commit

```text
多数 matchIndex >= N
且 log[N].term == currentTerm
才可以把 commitIndex 提到 N
```

D4 的中位数只实现了第一行。第二行是 Figure 8，不能省。

Follower 不算多数。它信 leaderCommit，并用 min 夹住。

```text
lastApplied <= commitIndex
apply 只能逐格加一
```

已提交前缀冻结。右边可以消失。

## 客户端

```text
不是 leader → 拒绝。课上没做“告诉你谁是 leader”
成功 = leader 已执行已提交命令。follower 可能还没 apply
答复丢失 + 重试 = 日志里两条。去重是应用序列号，§8
GET 也要证明你还是多数派里的 leader。心跳多数响应后再读
一台服务器不够提交。demo 返回 pending
```

## Key Invariants

```text
currentTerm 不减
每 term 每服务器最多一票
一个 term 最多一个 leader
leader 只追加
(index, term) 相同 ⇒ 前缀相同
commitIndex 不减，lastApplied 不减，lastApplied <= commitIndex
已提交条目在未来所有 leader 上
少数派不能提交
只提交当前 term 的条目
```

## Failure Handling

```text
复制前崩溃           条目消失，客户端重试，正确
复制到少数派后崩溃   未提交，可被覆盖，正确
提交后、答复前崩溃   条目还在，重试可能执行两次，应用去重
分区 A | B C        A 仍以为自己是 leader，但不能提交
                    B+C 选出 term 更高的 leader
                    愈合后 A 见到更高 term，下台
split vote           本 term 失败，随机超时再来
stale leader         更高 term 的响应迫使 step down
死 2/3 或 3/5        不可用。不要放宽多数
半死、单向连通       模型外。选举风暴。pre-vote 不在论文里
进程崩溃重启         若没落盘，和分区完全不同。本课未实现
```

## 消息异常

```text
延迟    term 过滤 + prev 检查
重复    同位置同 term 是空操作
乱序    没冲突不要删后缀
过期    拒绝并回自己的 term
```

## 他的实现顺序

```text
日志对象 → 固定 leader 的复制 → 心跳 → commit/apply
→ 当前 term 规则 → 选举
持久化、快照、改成员：不做
```

## 一句

Raft 复制的是日志。选举只负责在 leader 死后换一个不会丢掉已提交日志的写入源。Term 负责让旧写入源失效。多数派负责让分区两侧不能都成功。
