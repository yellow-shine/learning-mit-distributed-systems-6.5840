# David Beazley Rafting Trip：可独立学习的 Raft 教程

这套笔记不是字幕逐段摘要。它按问题把五天课重新组织成一条可以不看视频也走完的路径：单机挂了，为什么会变成复制日志，日志为什么比直接复制状态好管，然后才是 term、选举、心跳、AppendEntries、提交和应用。

材料来自课程全部字幕。讲师现场口误、ASR 误听、以及他明确说“今天不讲 / 我没做出来”的地方，都保留下来。Raft 论文、MIT 6.5840、etcd 工程细节如果字幕里没有，会单独标记，不会混进“他课堂上讲过”。

## 怎么读

1. 先读 [00-course-map.md](00-course-map.md)。先建立问题，再记机制。
2. 按 01 → 12 走协议。每一章都是：问题、一个自然但不够的方案、它怎么失败、Raft 怎么补、三节点例子、不变量、失败时怎样、他的代码落在哪、哪里容易写错、他真正想让你记住什么。
3. [13-code-evolution.md](13-code-evolution.md) 是这门课和其他 Raft 课最大的差别：他故意不从选举写起。
4. [14](14-experiments-and-debugging.md) 和 [15](15-common-bugs-and-invariants.md) 保留 demo、测不出来的 bug、以及他当场写崩的控制台。
5. 面试前只读 [17-cheat-sheet.md](17-cheat-sheet.md)。自测用 [18-self-check.md](18-self-check.md)，最后 10 题没有答案。

## 来源标记

| 标记 | 含义 |
| --- | --- |
| 【课程内容】 | 字幕里他讲过、画过、或演示过 |
| 【从课程推导】 | 他给了零件，结论是把零件接起来，不是他原句 |
| 【Raft 论文补充】 | 论文有、这门课没展开或只点到 |
| 【MIT 6.5840 实现补充】 | Lab 2/3 还要额外处理的实现问题 |
| 【工程实践补充】 | etcd / Consul / TiKV 等，课上没讲到的部分 |

引用格式：`(D2 2_part022)` 表示 Day 2 的 `2_part022.srt`。Day 1 的文件在子目录里，引用仍用 part 文件名。

## 文件

| 文件 | 内容 |
| --- | --- |
| [00-course-map.md](00-course-map.md) | 课程地图、问题→机制表、知识树 |
| [01-problem-and-replicated-state-machine.md](01-problem-and-replicated-state-machine.md) | 从单机到复制状态机 |
| [02-raft-state.md](02-raft-state.md) | 每个状态变量是谁的、要不要持久化 |
| [03-server-states-and-terms.md](03-server-states-and-terms.md) | Follower / Candidate / Leader 与 term |
| [04-leader-election.md](04-leader-election.md) | 选举、随机超时、投票限制 |
| [05-heartbeats.md](05-heartbeats.md) | 空 AppendEntries 不只是“我还活着” |
| [06-log-replication.md](06-log-replication.md) | 从客户端命令到复制 |
| [07-log-consistency.md](07-log-consistency.md) | prevLog、冲突删除、nextIndex / matchIndex |
| [08-commit-and-apply.md](08-commit-and-apply.md) | commit、current-term 规则、apply |
| [09-failure-scenarios.md](09-failure-scenarios.md) | 分区、旧 leader、提交前崩溃、客户端重试 |
| [10-safety-and-liveness.md](10-safety-and-liveness.md) | 五条 safety、liveness、FLP |
| [11-persistence-and-recovery.md](11-persistence-and-recovery.md) | 崩溃恢复；本周没实现的部分标清楚 |
| [12-concurrency-and-timers.md](12-concurrency-and-timers.md) | 事件循环、计时器、线程死了怎么办 |
| [13-code-evolution.md](13-code-evolution.md) | 五天代码是怎么一层层长出来的 |
| [14-experiments-and-debugging.md](14-experiments-and-debugging.md) | demo、实验、调试清单 |
| [15-common-bugs-and-invariants.md](15-common-bugs-and-invariants.md) | 课上的 bug 与课外汇总 |
| [16-paper-lab-and-systems.md](16-paper-lab-and-systems.md) | 论文、6.5840、真实系统、Raft 不负责什么 |
| [17-cheat-sheet.md](17-cheat-sheet.md) | 考前 10 分钟 |
| [18-self-check.md](18-self-check.md) | 问题链、四级练习、10 道不给答案的题 |

## 听写修正

字幕是自动听写。下面这些词在笔记里已经按语境改回，第一次出现时如果有歧义会注明：

- RAF / RAV / RAP / RAAF / RAS → Raft
- reader → 经常是 leader
- comment index → commit index
- NCD / MCD → etcd
- electrical leader → election / leader
- file → 有时是 follower
- Tokyo → Tokio
- taxis → Paxos
