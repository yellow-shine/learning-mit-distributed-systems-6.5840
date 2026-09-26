# 18 — 问题链与自测

先不看答案，把 Level 3 和 Level 4 写在纸上。最后 10 题故意没有答案。

## Raft Problem Chain

```text
单机崩溃，内存没了，怎么办？
↓
周期性快照。最近的命令仍会丢。

不想丢命令，怎么办？
↓
先写事务日志，再执行。崩溃后快照加重放。

日志盘也毁了，怎么办？
↓
把日志复制到别的机器。不要只复制最终字典：
字典没有 index，无法重试、无法对前缀、无法恢复到一半的写入。

多份日志谁来写，才不会分叉？
↓
一个 leader。客户端只跟它说话。Follower 彼此不说话。

Leader 崩溃或心跳消失，怎么办？
↓
Follower 的选举超时。先变 candidate，不是直接变 leader。

所有人同时超时，票分裂，怎么办？
↓
随机超时。失败的 term 作废，下一轮再试。
随机性只帮助活性，不提供安全。

旧 leader 复活，还在发命令，怎么办？
↓
Term。每次选举加一。消息里带 term。
更高的 term 迫使任何角色变成 follower。
更旧的消息拒绝，并把自己的 term 送回去。

为什么不能谁先醒来谁当 leader？
↓
它可能没有已提交的日志。
投票要看 lastLogIndex 和 lastLogTerm，不是只问愿不愿意。
先比末尾 term，再比长度。

为什么必须多数派，而不是一台备份？
↓
任意两个多数派相交。
提交用掉的那个多数，和选举用掉的那个多数，至少有一台两者都在。
3 要 2，5 要 3。少数派可以自以为是 leader，但不能提交。
读也不能绕过。

新 leader 怎么保证自己带着已提交条目？
↓
选举限制：日志至少和投票者一样新。
投票者若见过已提交条目，就不会投给一个没有它的候选人。

Follower 日志短一截、长一截、或中间 term 不同，怎么办？
↓
AppendEntries 携带 prevLogIndex 和 prevLogTerm。
对不上就拒绝。Leader 把该 follower 的 nextIndex 减一，再试。
接上之后，同 index 不同 term 才删除该条及之后。
没有冲突不能删。迟到的旧包会杀掉已接上的后缀。

(index, term) 为什么能代表整段前缀？
↓
同一任 leader 在一个 index 只写一条，且不改自己的日志。
能写下这一条，说明前一条的 index 和 term 已经匹配。
归纳下去，前缀相同。

什么时候可以执行？
↓
先提交，再按序 apply。
提交不是“复制到了多数”这一句就完。
还要 log[N].term == currentTerm。否则 Figure 8。

commitIndex 和 lastApplied 为什么分开？
↓
提交是“不会丢”的许可。执行是状态机的进度。
命令可以很慢，许可可以领先进度。进度不能领先许可，也不能跳格。

Leader 在提交后、答复前崩溃，命令还在吗？
↓
在。新 leader 必须有它。
客户端超时重试，可能再追加一条。只执行一次是应用的序列号，不是 Raft 核心。

网络分区怎么办？
↓
旧 leader 继续相信自己，直到看见更高 term。
它凑不齐多数，提交不了。
另一侧若有多数，选出更高 term，可以提交。
愈合后，更高 term 把旧 leader 打成 follower，冲突的未提交后缀被删。

无限分区或永远没有多数派，怎么办？
↓
安全仍在，进展没有。Raft 不破解 FLP。
活性依赖超时、随机、以及一段时间的网络稳定。

崩溃重启和分区为什么不同？
↓
分区保留内存。崩溃只保留落盘的 currentTerm、votedFor、log。
回复之前必须落盘。这门课知道这一点的重要性，没有实现。

日志无限增长、要换成员、要线性读，怎么办？
↓
快照、joint consensus、读之前用心跳证明领导权。
都不是 Figure 2 的复制循环自动给你的。这门课把它们留在范围外。
```

## Level 1 — Concept

1. Raft 复制的是什么？为什么不是把 KV 字典直接同步过去？
2. Term 是什么？它和日志 index 有什么不同？
3. Follower、Candidate、Leader 各在什么条件下进入？
4. 心跳是不是一条独立的 RPC？
5. commitIndex 和 lastApplied 各表示什么？为什么 `lastApplied <= commitIndex`？
6. 3 台和 5 台的多数派分别是多少？为什么不是“至少一台备份”？
7. 已提交和已执行有什么区别？客户端收到成功，说明哪一件已经发生？
8. 为什么集群通常是 3 或 5，而不是 50？

## Level 2 — Mechanism

1. 为什么选举超时必须随机？若三台都是 150ms，会发生什么？
2. RequestVote 为什么要带 `lastLogIndex` 和 `lastLogTerm`，而不是只问“投我吗”？
3. 末尾 term 不同时，为什么更短的日志也可以赢？
4. 空 AppendEntries 除了“我还活着”，还做哪三件事？
5. 为什么 leader 不能把同一份 AppendEntries 广播给所有 follower？
6. `prevLogIndex` 和 `prevLogTerm` 在检查什么？和发送者的 `term` 字段有什么不同？
7. 什么情况下 follower 必须删除后缀？什么情况下绝对不能删？
8. nextIndex 和 matchIndex 的区别是什么？组包时 prev 从哪个算？
9. 新 leader 的 nextIndex 为什么不能从旧 leader 继承？失败后为什么减一，而不是问 follower“你到哪了”？
10. 为什么提交还要求 `log[N].term == currentTerm`，多数复制不够？
11. Follower 自己没有 matchIndex 表，它怎么知道可以执行到哪？
12. 为什么 follower 收到更高 term 的**响应**也要下台，不只是请求？

## Level 3 — Failure Reasoning

 1. A 是 term=3 的 leader，和 B 断开，C 与 B 在一起。B/C 进入 term=4 并选出 B。A 还能往本地日志写吗？能提交吗？分区愈合后 A 必须发生什么？

 2. 五台，X 只在 leader 和一台 follower 上，leader 崩溃。X 可能消失吗？若集群改成三台，A 和 B 都有 X，结论还一样吗？

 3. Leader 已经提交并 apply，在回复客户端之前崩溃。客户端超时重试。状态机会不会执行两次？这是 Raft 的 bug 吗？

 4. 分区两侧分别把 x 提交成 13 和 42。只从“已提交前缀”回答 GET，为什么仍可能读到两个值？

 5. 两台机器互相选举，谁也当不了 leader。这是实现错了，还是协议要求的？

 6. 一台机器能把 RequestVote 送出去，但收不到任何心跳。另外两台本来健康。会发生什么？论文的假设哪里被破坏了？

 7. 一条只含 C1 的 AppendEntries 在网络里耽搁。含 C1 和 C2 的包先到并已经接上。迟到的包到达时，如果代码把“已存在的 index”都当成冲突并删后缀，会破坏哪条不变量？

 8. 候选人日志末尾 term 更高但更短。所有更长但 term 更旧的投票者应该怎么投？若有人把比较写成“先比长度”，会选出什么样的 leader？

## Level 4 — Implementation

 1. AppendEntries 失败后，leader 为什么要减小 nextIndex？减到 0 以下，在 Python 里会额外发生什么？

 2. 成功响应只有一个布尔时，leader 怎样更新 matchIndex？把 matchIndex 放进响应里，和论文 Figure 2 是什么关系？

 3. 用 matchIndex 的中位数算 commitIndex，5 台的 `8,5,8,2,7` 应该得到多少？这个算法还缺 Figure 8 的哪一个判断？

 4. `prevLogIndex = nextIndex` 和 `prevLogIndex = nextIndex - 1`，哪一个对？错了的那个会让 follower 去对哪一条？

 5. 选举计时器应该在哪些事件重置，不应该在哪些事件重置？

 6. 为什么 `handle_message` 里面不能同步 `send` 并等待回复？这和“持有锁发 RPC”是什么关系？

 7. 崩溃恢复时，为什么 `votedFor` 必须在发出赞成票之前落盘？只在 apply 时把 KV 字典写盘，漏了哪三类状态？

 8. 单节点 leader 上 `set x 42` 返回 pending。这是 bug 吗？要看见 commit，最少还要什么？

 9. 假网络上两份日志能收敛，为什么不能推出真 socket 实现是对的？反向为什么成立？

10. 你准备在 `handle_message` 前后放哪三条断言？它们抓不到哪一类仍然可能的安全事故？

## 最值得想清楚的 10 个问题

不给答案。能讲给别人听，才算想清楚。

1. 如果已经要求多数复制，为什么还需要 current-term 提交规则？把 Figure 8 里“看起来已经在多数派上”的那条，和“真正提交”的那条分开讲。

2. 旧 leader 可以一直认为自己是 leader。为什么这仍然不破坏 Safety？它具体在哪一步被挡住，挡住的是写入、复制，还是提交？

3. 为什么候选人 term 更高，也不应该自动得到选票？更新 currentTerm 和投出 `votedFor` 是两件什么不同的事？

4. 为什么 `(index, term)` 足以判断前缀相同？如果 leader 被允许修改自己的旧条目，这个指纹还会成立吗？

5. 为什么 committed 和 applied 必须分开？如果合成一个变量，慢命令、崩溃、以及“答复丢失但命令已执行”会分别坏在哪里？

6. 选举限制保证新 leader 有已提交日志。这个保证依赖提交规则。若实现用 D4 的中位数、没有 term 检查，选举限制还会保证什么？

7. 心跳若只带“我还活着”，不带 prevLog 和 leaderCommit，会有哪两种看起来像活性、实际是安全或进展的故障？

8. 客户端重试和只读，为什么都不能在 Figure 2 的复制循环里被顺便解决？它们各自还缺哪一个事实？

9. 这门课把选举放在最后。若按论文和 6.5840 先做选举，你会在哪一个时刻误以为系统已经完成？

10. 一个断言“已提交条目没有变化”通过了 10 万次随机测试。你还没有资格相信什么？Beazley 把最后一个 bug 留在代码里，是在拒绝什么幻觉？
