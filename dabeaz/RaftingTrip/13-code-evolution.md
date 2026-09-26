# 13 — 代码是怎么一步步长出来的

这是这门课和“先讲选举、再讲复制”的教材之间最大的差别。顺序以字幕为准。项目编号他有时改口，下面同时记下编号和他当天实际在写的东西。

总形状：

```text
问题
  ↓
一个小到能测的机制
  ↓
这个机制暴露的下一个问题
  ↓
下一个机制
```

他反复拒绝的捷径：从论文状态机人人都是 follower 开始；从选举消息开始；在复制还是坏的时候把选举堆上去。

## D1 — 还没有 Raft 代码

### 问题梯子，然后 etcd

新增：没有代码。新增的是问题。单机崩溃、快照丢最近命令、事务日志、日志盘也会毁、一份备份不够、Figure 1 要求相同命令序列。

为什么现在：不先有应用模型，分布式机制没有东西可复制。

暴露的下一个问题：多数派、客户端必须认识所有地址、只读也不能绕过。etcd 三台死两台，GET 超时 (D1 1_part007–1_part009)。

### Warmup：能把一条消息送到另一台

新增：send / receive。实现随意：socket、ZeroMQ、FastAPI、Tokio。他没有偏好。

为什么现在：没有网络就没有分布式。这是最脏的一层，可能要好几天。今天写得很差，后面还有几天修 (D1 1_part010–1_part011)。

不要做：不要用这一小时去实现论文状态机。

### Project 1：没有 Raft 的 KV

新增：

```text
KV.run_command("set X123")     无 socket，REPL 可测
execute_command(bytes)->bytes  编码
Server                         socket，取消息，调用，送回
```

为什么现在：Figure 1 的状态机得先存在。他们已经有步骤 1、3、4，缺的是步骤 2。

暴露的下一个问题：命令不该一到就执行。要先 staged 到日志里，复制到足够多的机器。不是 leader 要拒绝客户端。中间是 magic (D1 1_part040–1_part041)。

快照学生已经在试，他没写，不裁判。

### Visualizer：步骤 2 不是函数调用

新增：观察，不是代码。

- AppendEntries 请求和回复。
- Leader 周期发送。死掉的 server 4 被无限重试，恢复后被补上。没有重试上限，直到自己不再是 leader。
- 多条 RPC 同时在飞。
- 边上的倒计时：听到 leader 就跳高，然后下降。停掉 leader，最小的那个到 0，发起选举，变成 leader。
- 要赢还得有足够的日志。算法当天不讲。
- 两个接近的超时会同时变 candidate。

为什么现在：让人看见“请求-响应”模型不够，再去写 traffic light。

暴露的下一个问题：这种同时有计时器、消息、重试的程序，结构是什么？

### Project 2：红绿灯，准备扔掉

新增：`traffic/light.py` 不改。自己写 `traffic.py`。逻辑类只有 clock、颜色、按钮。他演示的规则：东西向绿、南北向红，且（clock>30 或（clock>15 且按钮按下）），东西向变黄，时钟归零，按钮清掉。

控制器拥有 socket。禁止 `sleep(30)`。事件队列、socket 超时、ASIO completion handler 都可以，他不选。

测试形状：构造时注入 clock=29；一个 `handle_event`；出口检查两种颜色不能相同，也不能一绿一黄；fuzz 能抓到“27 秒且按钮按下则两边变绿”的故意 bug。

为什么现在：他希望 Figure 2 以后也能离开 socket 被测。

暴露的下一个问题：Raft 的第一行到底是环境、Figure 1 的数据流，还是单独的 Figure 2？家庭作业，没有标准答案 (D1 2_part040)。另外去读论文第 5 节。

学生把“是否该变”和“怎样变”拆开，代码翻倍，清晰度减半。他让思考先于代码 (D1 2_part039)。

## D2 — 固定 leader，先让消息和日志对象存在

### 从一台到两台

新增：一个固定 leader，一个 follower，follower 克隆 leader。没有选举。

为什么现在：一步从 1 到 2 是难的。2 到 3 不难。选举、找 leader、改成员都还不要 (D2 1_part000)。

学生已经独立得出“固定 leader 更好测”。

### Project 3：集群能说话

新增的结构，当天从草图长成能跑的 chat：

```text
配置: 节点号 → 地址。文件或字典。他不选定。
网络: listen + 队列 + send。receive 从队列 pop，因为不知道谁会说话。
      对方死了，网络层丢掉消息，不在这里重试。
RaftServer: 身份 + network + logic + log + app
run: 取消息，解码，交给 logic，把返回的列表发出去
```

Chat 被他塞进 logic，而不是留在网络旁边。这是早上骑车时想到的，他以前没这么写。控制台发 chat，不等待。`handle_chat` 打印，并 `self.send` 一条响应。`self.send` 只追加到列表。

演示：对端打印 “1 said hello”，发送端打印 “2 got hello”。

为什么现在：他不在乎聊天。他在乎收、回、发、看见它跑。以后的 AppendEntries 就是这个形状。学生说确认是共识的积木，所以投票再花一小时做这个，再做 Project 4。

被放弃或推迟的设计：把 socket 塞进消息；发送后阻塞接收；逻辑持有网络对象（学生称为此时的人为依赖）；每条消息新连接（能看见一条就行，稳态不行）。

暴露的下一个问题：响应和请求不配对。若需要关联，把原消息或以后的 term、commit 号放进响应。计时器仍然没放进这张图 (D2 1_part027)。

### Figure 1 的时序，用代码词汇再说一遍

新增的概念，还没有提交代码：

- Leader 数 OK。够了就执行并回复客户端。够了，在早上是“配置好的集群”加上学生说的过半。他没有推导公式。
- Follower 稍后执行。后续 AppendEntries 告诉它提交到哪。
- commitIndex 是已复制到可以安全执行的最高点，外加他没列的几条约束。
- lastApplied 追着它。论文只说若 commit 领先就加一并应用。何时检查，归你。
- 已提交即冻结。日志条目持久，崩溃不能丢，也不会被改掉。怎么存，没说。
- 慢命令会让后面很多命令在 Raft 里 pending。

暴露的下一个问题：那些“额外约束”是什么？当天不讲。那就是 D5 的 Figure 8。

### Project 4 开始：日志上的怪 append

新增：一个没有网络的日志操作。玩具签名是 previous index、previous item、new item。

它必须：

- 重复调用不改变日志
- 拒绝洞
- 前一项不一致就拒绝
- Figure 2：已有条目冲突则删除该条及之后

为什么现在：AppendEntries 是宇宙中心，因为 Raft 的目的就是复制日志 (D2 2_part026)。Chat 刚证明 logic 可以测。现在把测的层次再降一层，连消息都不要。

Term 可以伪造。不要为了得到 term 去实现选举。

他当天没写完。55 分钟结束时日志还不能用。他说这没关系。

暴露的下一个问题：为什么要比 previous term，而不只是 index？

### 分区白板 C3/C4

新增：一个没有跑起来的场景。C1 C2 已复制。C3 只到达 leader 和一台。切开。另一侧三台选出新 leader，写入 C4。旧 leader 仍自认为 leader。两边用 term 区分。AppendEntries 必须带前一条的 term，对不上就拒绝。冲突后缀要删。

愈合怎么走、哪边日志更 up-to-date，留在 Figure 2 的句子上，没有单步演完。

### 索引坑

新增的警告，不是功能：

- 论文 1-based。
- 论文说 last log index，不说 length。
- 条目里的 index 和数组下标在截断之后不是一回事。
- Python `log[-1]` 会把 off-by-one 变成“神秘地读到最后一条”。他见过这造成极难查的错误。

他想过日志对象只留最后 100 条。自己标成可能很可怕。快照是第 6 或第 7 节，他记不清，本课不做。成员变更是另一节，也不做。不要把两节并成一个节号。

## D3 — 复制，仍然没有提交，没有选举

### 两个写操作

新增：

```text
append_new_command(term, command)          leader，总是成功
append_entries(prevIndex, prevTerm, entries, ...)
                                           follower，可以失败
```

为什么现在：一个 append 说不清客户端命令怎么变成条目，也说不清 prev 和一批条目。

日志对象创建时必须有初始条目。Poison/dummy 让 index 0 存在。重启不要再插。持久化不做。

### 不是广播

新增：`update_single_follower(id)`。Visualizer 里同时飞出的包，prev 不同，有的是空 entries。

谁调用它：计时器、追加后立刻、或队列。他不知道。只要求最终发出去。这个洞留到 D4，仍未合上。

### 冲突删除，以及不要误删

新增的规则细节：

- 分区造成两个 leader，日志分叉。当前 leader 的命令赢，冲突后缀吹掉。
- C3 只在两台，不是多数，客户端没得到成功。吹掉 C3 不是收回承诺。
- 迟到的只含 C1 的包，不能删除已经接上的 C2。没有冲突就不删。Figure 2 的 “if” 是字面意思。

### nextIndex

新增：每个 follower 一个。`prevLogIndex = nextIndex - 1`。成功才增加。死人停在旧值，RPC 一直打那个点。

新 leader 不继承表。初始化成自己的下一位置。Visualizer：设成 7，对只有 2 条的机器失败，7、6、5、4 往下减。问 follower 到哪了，论文说可以优化，作者觉得没必要。他买这个实用主义。

暴露的下一个问题：成功时 nextIndex 加多少？布尔不够。

### matchIndex

新增：visualizer 回复里有 match index。`nextIndex = matchIndex + 1`。TLA 把它定义成 prev 加上成功追加的条数。

他读 Figure 2 的结果，没看到这个字段，说论文没提。精确情况见 [07](07-log-consistency.md)：状态里有，RPC 结果里没有，leader 可以自己算。

下界：不能减过左端。TLA 有，他的代码没有。

### 提交被命名为 Project 6，然后推开

学生问 commit index 是不是等所有回复。不是。是多数。Follower 的公式在接收规则第 5 点，他不读。Leader 的句子“措辞很怪”。今天不实现。明天仍主要是复制。

### 假网络

新增：两台 RaftLogic，`send` 进列表，手工 `handle`。Figure 7：更短、更长、term 冲突。放一条新命令，允许多轮，断言日志相等。

早上的版本不能跑，缺 become-leader 和相等断言。下午他说能跑。还没上真 socket。

假网络失败 ⇒ 真网络失败。反过来不成立。

收工前种下两粒种子，不做完：五台字典集群，按目的地投递直到没有外出消息，用 Figure 6 断言大家相等；控制台能 show log、提交 X42、手动 update。

选举放到周五。快照讲了 Roblox 的 19.8 秒，然后明确忽略。

## D4 — 心跳、提交、应用接线

### 测试策略先于新功能

新增的纪律：不要透过 socket 测日志。先确定性收敛，再在五台且有计时器之后丢消息。丢消息是 fuzz，难调，不是证明。TLA 回答“Raft 对不对”，不回答“我的代码是不是 Raft”。TLA demo 答应了，这天没做。

学生的真实 bug：一对一的保存/更新/确认看起来对。停掉 follower，leader 继续加命令，follower 回来，index 怪了。他没有逐行修。他用 nextIndex、缺口、回退、Figure 7 必须收敛来回答。

另一个学生：日志测完了仍不信，因为论文没说哪些状态合法。他的 oracle 仍是最终相等，不是一张合法状态表。

### 心跳

新增：空 AppendEntries 就是心跳。不要单独的心跳消息。不要每条客户端命令喷一次。可以批量，论文只说 may。间隔小于选举超时，不必小于 ping。可以按 follower 不同。他不知道 etcd 怎么做。

他的控制台 `update` 是用手按的心跳。演示时控制台崩了。

触发点仍未定。见 [12](12-concurrency-and-timers.md)。

### 全活或全死

新增：线程异常就 `os._exit`。Cloudflare 单向故障造成选举风暴。pre-vote 不在论文里，不布置。拜占庭句子他没在录音里找到原文。

### Project 6，然后改口叫 5 和 6

新增：

- 响应里记下 matchIndex。
- 排序，含 leader，取中位数，当作 commitIndex。
- 发出 AppendEntries 时带 leaderCommit。
- Follower 成功且落后时，`commitIndex = min(leaderCommit, 自己最后一条)`。
- Server 循环里 while lastApplied < commitIndex：加一，执行。
- 执行第一版是打印。

推迟：current-term 才能提交。黑板上一个问号。故事是 D5 Figure 8。

### 应用接线失败得富有教育意义

新增的尝试，没有一个做完：

```text
单节点 set x 42 → pending（没有多数）
拉起更多节点 → 打印 applying，仍不是 apply
concurrent.futures 的类比 → future 穿不过函数边界
callback → execute 得到 okay，okay 到不了客户端
UUID 轮询 → 草图
学生的 client→index 字典 → 他建议用事务号
```

三客户端同时 `set x=13`、`set x=37`、`get x`。任何到达顺序都可能。Raft 只保证所有副本看到同一顺序。应用是错的，就会统一地错。

分区后重试会把 C1 放进日志两次。§8 的序列号是应用的事。

为什么现在就讲这个：Figure 1 的箭头 3 论文没说怎么接。不讲的话，提交了也无法看见 KV 变化。

暴露的下一个问题：选举，以及 current-term 规则。他说选举比接线疼得轻。固定 leader 若能把命令路由走通，已经算成功 (D4 2_part042)。

## D5 — 选举、Figure 8、然后承认做不完

### 成功标准先降下来

三台，server 1 写死为 leader，命令能复制出去，就是合理的成功。接线是整个项目最难的部分，没开始可以先不管。选举更容易，但不要堆在坏的复制上。

### Term 和 RequestVote

新增：

- 每条消息带 term。更新则下台；更旧则拒绝并把自己的 term 送回去。
- 超时 → term+1 → candidate → 投自己 → RequestVote(lastLogIndex, lastLogTerm)。
- 每 term 一票。日志至少一样新：先比 lastLogTerm，再比长度。他第一遍说错，改过。
- 未提交尾巴可以被新 leader 覆盖。
- 当选后立刻 AppendEntries。收到当前 leader 的 AppendEntries 要重置超时。
- 随机超时是活性的关键。

Figure 6：S1 该赢，S2 不该赢。他没读出图的内容。

他的代码上午选不出 leader。下午三节点 demo：两台赢不了，三台能，杀掉 leader 会再选。中间修了什么，没说。

### 不变量进代码

新增：`handle_message` 前后，term 和 commitIndex 不减；最后一条已提交条目不变。

这来自 D1 红绿灯“非法状态不可出现”。学生建议用 Some/None 让 follower 不能携带 nextIndex 表。他同意方向。

五年左右每次重写都丢已提交条目，只在大约一百万次随机模拟里被这个断言抓住。他当时完全没料到。

### Figure 8

新增：提交规则补上 `log[N].term == currentTerm`。

他用一场崩溃、短命的 term 3、更高 term 的复制、然后旧节点复活覆盖，说明“看起来复制到多数”不够。细节和论文插图的差异见 [08](08-commit-and-apply.md)。

他不知道自己能否在不知道 Figure 8 存在的情况下写出能抓住它的测试。前提是先有“已提交不得消失”的检测。他用两年、很怪异的调度，才在关掉检查的实现上撞到一次。

下午的 fuzzer 在检查已经加回去之后又响了。那是另一个 bug。他留着。

混沌测试的菜单：丢、重复、乱序洗牌。大约 10 万次。他拒绝发放现成测试套件。会测，是项目的一部分 (D5 2_part009)。

### 客户端和 GET

新增：命令带 UUID，pending 表，apply 时用线程通知。他觉得绕。

GET 不能本地读。分区两侧 x=13 和 x=42。等待区加一轮多数心跳，才执行读。没实现。etcd 单台 GET 超时是这个规则的外部样子。

### 明确不做

持久化：学生以为 apply 时写盘。他说那不是答案，掉电和写在哪一层都没谈。

快照：not now。

第 6 节 joint consensus：超出他当时的脑力。不实现。

## 若把这条演进收成实现清单

按他的顺序，而不是按论文目录：

```text
1.  KV，无网络核心
2.  能收发的网络，队列把 accept 和逻辑隔开
3.  server 循环：消息进，消息列表出
4.  日志对象：按位置写，拒绝洞和冲突，冲突才删后缀
5.  固定 leader，per-follower nextIndex，失败减一
6.  成功更新 matchIndex，假网络把两份日志跑到相等
7.  空 AppendEntries 当心跳，计时器在状态机外面
8.  中位数或等价扫描计算 commitIndex
9.  leaderCommit + min，lastApplied 循环
10. 只提交当前 term
11. term 检查放进每条 RPC
12. RequestVote 和 up-to-date
13. 应用层请求 ID、读的领导权证明
14. 持久化三元组，回复之前落盘     ← 课上未做
15. 快照                              ← 课上未做
16. 成员变更                          ← 课上未做
```

每一步都是因为上一步在故障下暴露了洞，不是因为论文下一节在那里。
