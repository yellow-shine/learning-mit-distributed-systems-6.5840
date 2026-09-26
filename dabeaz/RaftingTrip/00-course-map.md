# 00 — 这门课到底在解决什么，以及整门课的地图

## 先看问题，不要先背名词

【课程内容】Beazley 第一天没有从 term 讲起。他从一张很普通的图开始 (D1 1_part002–1_part005)：

```text
Client
  |
  v
Server
  |
  v
State
```

一台机器就能跑。命令一个一个来，一次只执行一条。他用的例子是按键、学校作业、以及后来的 `set name Guido` / `set x 42`。问题立刻出现：

- 进程崩溃，内存里的状态没了。作业写到一半停电，纸还在不在？
- 每分钟做一次快照，最近几十秒的命令不在快照里。
- 于是先把命令写进事务日志，再执行。崩溃后加载快照，重放日志。数据库一直这么干。
- 日志本身也会烧。磁盘坏了，或者他举的韩国政务云那次火灾，备份不存在，重放就没了。
- 所以日志也要复制到另一台机器。
- 备份机硬件挂了，主程序还能跑吗？学生说“也许读可以、写不行”。他的回答是：如果备份的意义是不丢命令，没有备份还继续跑，就是不安全运行。你最不想看到的是备份先死，然后主程序也死，中间那批命令两边都没有。

到这里，系统变成：

```text
          +--> Server A   app + log
Client ---+--> Server B   app + log
          +--> Server C   app + log
```

新问题不是“多买几台机器”，而是：

- 谁负责接客户端？
- 三台怎么执行同一串命令？
- 消息会迟、会丢、会重复、会乱序。
- 机器可以死，也可以没死但说不了话（分区）。
- 两台都觉得自己是负责人时怎么办？

【课程内容】论文 Figure 1 就是这个目标的画法：多份应用执行**相同顺序**的命令。做法不是把最终的字典复制来复制去，而是让每台机器挂一份**相同的日志**。日志相同，确定性状态机就会走到同一个状态 (D1 1_part004–1_part005)。

这就是 replicated state machine，共识只是让日志相同的那个机制。他后来说得很直 (D5 1_part006)：

> Raft 解决的问题不是选举。核心是复制日志。没有日志复制，前面做的都没有意义。

所以这门课的顺序和论文目录、也和 MIT 6.5840 Lab 2A→2B 相反：先把日志和复制做出来，选举放到最后一天。他甚至说想跟 Ousterhout 吵一架，因为 Stanford 那门课从选举开始 (D5 1_part007)。

## 为什么这很难，难的又不是公式

【课程内容】论文标题里的 understandable，他觉得这个词干的活太多了。学生补了一句：是相对 Paxos 而言。他同意：相对好懂，不等于简单 (D2 1_part000)。

难在三层，而且他反复说第三层才是这门课的真正主题：

1. 协议规则短，但每条规则都在堵一个你想不到的历史。Figure 8 那种“看起来已经复制到多数，其实不能提交”的序列，他觉得人脑不会在调试时撞上 (D5 2_part000–2_part008)。
2. 规则要在崩溃、分区、重试、乱序里同时成立。Visualizer 里领导同时在给一台 follower 往前推、给另一台往回退 (D4 1_part003)。
3. 真正把人毁掉的是软件结构。七年前他用 ZeroMQ 写到一半，发现代码在经典意义上不可调试。三年前两个加起来约 60 年经验的工程师结对，第四天互相不说话，代码报废。这是他见过的唯一一次结对编程离婚 (D1 1_part001–1_part002)。课程大量时间花在：先写哪一行、怎么把逻辑从 socket 上撕下来、怎么在没有网络的情况下断言日志相等。【课程页面，不是口播】五台机器的配置里，线程、计时器和队列叠在一起，容易超过你能同时看清的范围。

周末结束时的成功标准故意定得很低 (D5 1_part000)：三台机器，哪怕 leader 写死是 server 1，一条命令能复制出去。应用把结果送回客户端，他称为整个项目最难的噩梦，可以先不做。持久化、快照、成员变更，明确不做。

## 问题 → 机制

每一行都是“这个机制在堵哪个具体洞”。细节在后面各章。

| 问题 | 如果没有这个机制会怎样 | Raft 机制 | 课上何时出现 |
| --- | --- | --- | --- |
| 多副本要执行同一串命令 | 直接同步最终状态，崩溃窗口里状态对不上 | 复制命令日志，而不是复制字典 | D1 问题梯子 |
| 谁协调 | 客户端自己扇出，失败时没人知道写到了哪 | Leader | D1 Figure 1，D2 固定 leader 先做 |
| Leader 挂了没人接 | 集群停在那里 | Election timeout → Candidate | D1 visualizer，D5 才写代码 |
| 所有人一起超时 | split vote，票分散，选不出人 | 随机 election timeout | D1 演示接近同时超时；D5 说随机性是活性的关键 |
| 旧 leader 复活还在发命令 | 两个写入源 | Term；更高 term 迫使 step down | D2 用 term 区分 C3/C4；D5 写成规则 |
| 随便谁都能当 leader | 缺了已提交条目的人当选，然后覆盖 | 选举限制：日志至少一样新；每 term 只投一票；多数派 | D5 |
| Follower 落后或分叉 | `list.append` 重试会把 `set x 42` 写两遍，或在中间留洞 | AppendEntries + prevLogIndex/prevLogTerm | D2 玩具 append，D3 Figure 2 |
| 各 follower 落后程度不同 | 一次广播同一包，死掉的机器永远对不上 | 每 follower 一个 nextIndex，失败就减一 | D3 visualizer |
| 成功回复只有一个布尔 | leader 不知道 nextIndex 该跳到哪 | matchIndex（论文里是 leader 自己更新；课上 visualizer 把它放进了回复） | D3 |
| 少数派也算数 | 分区两侧都能提交，愈合后历史矛盾 | Majority：3 节点要 2，5 节点要 3 | D1 etcd；D2 分区 |
| 复制了就可以执行 | 客户端已看到成功，条目后来被删 | commitIndex；且只能提交当前 term 的条目 | D4 先做多数派；D5 Figure 8 补 term 规则 |
| Follower 不知道别人的 matchIndex | follower 自己猜提交点 | leaderCommit 字段，follower 取 min | D4 |
| 提交了但还没执行 | 把“已共识”和“已改状态机”混成一个变量 | lastApplied 追着 commitIndex 走 | D2 读论文；D4 写成 server 循环 |
| 心跳如果是另一条 RPC | 多一套协议，还是漏掉提交点传播 | 空 AppendEntries 同时负责保活、压选举、带 term、带 commitIndex | D2/D4 |
| 网络分区 | 旧 leader 仍自认为 leader，但凑不齐多数 | 多数派交集 + term | D1 概念，D2/D4/D5 场景 |
| Stale RPC | 迟到的 C1 把已经复制的 C2 删掉 | 只有冲突才删后缀；term 更旧则拒绝 | D3 |
| 客户端超时重试 | 同一条 set 进日志两次 | 应用层请求 ID；Raft 不管 | D4 §8，D5 UUID |
| 只读也打到旧 leader | 分区两侧 GET 看到不同的 x | 读也要证明自己还是多数派里的 leader | D1 etcd 超时；D5 §8 |
| 崩溃后丢 term / 投票 / 日志 | 同一 term 投两次，或已提交条目消失 | 持久化 currentTerm、votedFor、log | 课上讲了必须持久，**没有实现** |
| 日志无限增长 | 磁盘满 | Snapshot / compaction | 讲了为什么难，**本周不做** |
| 半死节点一直发投票 | 选举风暴打满健康节点 | 论文假设全死或全活；pre-vote 是扩展 | D4 Cloudflare |

## 知识树

```text
Part I    单机 → 事务日志 → 复制日志 → 复制状态机          01
Part II   节点状态：谁拥有、要不要落盘                      02
Part III  Follower / Candidate / Leader                   03
Part IV   Term                                            03
Part V    Leader election                                 04
Part VI   Heartbeat = 空 AppendEntries                    05
Part VII  日志复制数据流                                   06
Part VIII AppendEntries 每个字段                          06, 07
Part IX   Log matching、冲突、回退                         07
Part X    nextIndex / matchIndex                          07
Part XI   Commit，以及 current-term 规则                  08
Part XII  commitIndex 与 lastApplied                      08
Part XIII 失败场景                                        09
Part XIV  Safety                                          10
Part XV   Liveness                                        10
Part XVI  FLP（课上没讲，单独标出）                        10
Part XVII RPC 与迟滞/重复/乱序                            06, 07, 17
Part XVIII Timer                                          05, 12
Part XIX  并发模型                                        12
Part XX   Persistence（讲了，没做）                        11
Part XXI  Crash recovery                                  11
Part XXII 代码演进                                        13
Part XXIII Demo                                           14
Part XXIV Debugging                                       14
Part XXV  常见 bug                                        15
Part XXVI 不变量                                          15
Part XXVII MIT 6.5840                                     16
Part XXVIII 论文 Figure 2                                 16
Part XXIX 真实系统                                        16
Part XXX  Raft 不负责什么                                 16
```

## 五天实际在干什么

课没有幻灯片。每天是讨论、看 visualizer / etcd、然后长时间自己写。下面是教学顺序，不是论文顺序。

| 天 | 他想让你离开时拥有什么 | 明确还没有的 |
| --- | --- | --- |
| D1 | 问题梯子；etcd 上看到多数派和“客户端必须认识所有地址”；KV 应用分层；traffic light 把规则从 socket 撕开；visualizer 里看见 AppendEntries、无限重试、随机超时、两个 candidate | 任何 Raft 代码 |
| D2 | 固定 leader；Raft server = 网络 + logic + log + app；chat 走通“消息进、消息列表出”；AppendEntries 不是 `list.append`；分区画出 C3/C4 | 选举；能工作的日志对象（他当天没写完） |
| D3 | 两个写日志操作；per-follower nextIndex；冲突才删后缀；新 leader 乐观地把 nextIndex 设成自己的，失败再减；假网络把 Figure 7 跑到日志相等 | commit（这是 Project 6）；选举（放到周五）；快照 |
| D4 | 心跳就是空 AppendEntries，而且不要每条客户端命令都发一次；用 matchIndex 的中位数算 commit；follower 用 leaderCommit；lastApplied 在 server 循环里追；客户端回包是噩梦 | current-term 提交规则（他打了问号）；选举 |
| D5 | term 规则；RequestVote 和“谁的日志更新”；Figure 8；三节点选举 demo；不变量和混沌测试；GET 不能本地读 | 持久化、快照、joint consensus；他自己的选举上午还选不出 leader；fuzzer 还抓到一个他决定留着的 bug |

项目编号在不同天有重叠，以他当天的用法为准，详见 [13-code-evolution.md](13-code-evolution.md)。

## 他反复打的钉子

这些句子后面会反复用到。先放在这里，避免后面每章都重新发现一次。

1. 复制的是日志，不是应用的最终状态。选举是日志复制的善后，不是起点 (D5 1_part006–1_part007)。
2. 客户端只跟 leader 说话。Follower 彼此不说话 (D2 1_part001–1_part002)。
3. 一步从一台机器到两台，比从两台到三台难 (D2 1_part000)。
4. AppendEntries 是宇宙中心 (D2 2_part026)。
5. 已提交的前缀冻结。提交点右边什么都可能发生，包括被删 (D4 2_part000，D5 1_part004)。
6. 论文几乎不告诉你“什么时候”做某件事。它只告诉你这样做是安全的 (D2 1_part004)。
7. 能测的形状是：一条消息进 logic，一组消息出来。socket 留在最外层 (D2 2_part019，D4 1_part000)。
8. 服务器要么全活要么全死。半死不在模型里 (D4 1_part033)。
9. Raft 不修理你的应用。应用是错的，它会让所有副本以同一种方式错 (D4 2_part040)。
10. 看自己的代码觉得可怕，说明项目做对了 (D5 2_part037)。

## 这门课没有覆盖、后面章节会标明的东西

- 没有实现持久化，也没有把“投票必须先落盘再回复”讲成一条实现规则。
- 没有实现 snapshot / InstallSnapshot，尽管 D3 花了不少时间讲它为什么会毁了一个星期。
- 没有实现 membership change。他读过论文第 6 节，说超出自己当时的脑力 (D5 2_part039)。
- 没有系统讲五条 safety 的名字。安全性质是从例子里长出来的，命名在 [10](10-safety-and-liveness.md) 里标成论文补充。
- 没有讲 FLP。活性依赖超时和“网络最终会安静一会儿”，这一点他用随机超时和“无限分区就不能保证进展”讲过现象，理论名字是补充。
- 没有给出选举超时的毫秒数。不要把论文里的 150–300ms 写成他的课堂常数。

## 一条可以背下来的问题链

完整版在 [18-self-check.md](18-self-check.md)。最短版：

```text
单机挂了 → 不能只靠快照 → 事务日志也会毁
→ 复制日志，而不是复制最终状态
→ 谁写日志：只要一个 leader
→ leader 挂了：超时，变 candidate
→ 怎么认出旧 leader：term，更高的赢
→ 为什么不能谁都当：多数派，且日志必须够新
→ follower 日志不一样：prevLogIndex/prevLogTerm，冲突就删后缀
→ 何时能执行：多数派复制，并且这条是当前 term
→ 已提交的会不会被新 leader 丢掉：选举限制 + 多数派交集
→ 分区：旧 leader 可以继续自以为是，但提交不了
→ 客户端重试、只读、快照、改成员：都不是 Figure 2 自动送你的
```
