# 12 — 计时器、线程，以及为什么逻辑里不该有 socket

## 1. Problem

Raft 同时有：

- 多台机器上的连接
- 选举倒计时
- 心跳周期
- 客户端阻塞等待
- 复制 RPC 的在途请求

D1 看完 visualizer，他的判断是：熟悉的“一个客户端、一个服务器、发完等回复”太简单 (D1 2_part002)。Leader 同时发出多条 AppendEntries，回复各自回来。这已经意味着线程或其他并发机制 (D1 2_part001)。

【课程页面，不是口播】五台机器的配置里，线程数量可以到几十。口播里他强调的是结果：你很快会失去对正在发生的事的把握。

## 2. Naive Solution

- 在状态机里 `sleep(选举超时)`。Traffic light 已经否定了这个形状：`sleep(30)` 期间按钮按了也没人理 (D1 2_part018)。
- 每个 peer 一个线程，线程里保存“我发到哪了”，然后阻塞等那一台的回复 (D1 2_part003 学生的直觉)。
- `send` 之后立刻 `recv`，把响应和请求配成一对 (D2 2_part002)。
- 计时器放在 logic 里读墙钟，网络层再读一次墙钟。
- 一个线程死了就让别人继续跑，方便调试。
- 用 daemon 线程绑生命周期。他不相信一个 daemon 死了进程会退出 (D4 1_part032)。

## 3. Why It Fails

**send 后立刻 recv。** 下一条消息可能来自另一台，不是你在等的那台。接收循环被锁死 (D2 2_part006)。Raft 的响应是以后到达的另一条消息，不是这次调用的返回值。

**在 logic 里持有 socket。** 每个测试都要 mock 网络。学生指出这一点，他同意，所以不想要这个依赖 (D2 2_part005，2_part019)。

**阻塞连接。** 每条消息新建 TCP，心跳路径上每秒上百次三次握手。连接失败时 TCP 可以坐两分钟。他问这会不会冻住整个系统，没有回答 (D2 1_part025)。

**线程死而不退出。** 症状像死锁或“Raft 没按我想的做”。真正的问题是有一部分已经不在跑。他在这个项目上丢过很多调试时间 (D4 1_part032–1_part033)。

**控制台从外面 submit。** D5 flood 时他怀疑 `send` 不是线程安全的。状态机的入口如果有两个，不变量就没有单写者 (D5 1_part041)。

## 4. Raft Solution

他没有冻结一个框架。他冻结的是边界。

```text
Network threads / accept loop
        |
        |  bytes 推进队列（Rust: channel）
        v
Server run loop                ← 唯一改 Raft 状态的地方（他的目标，不是他写成锁的定理）
        |
        |  decode
        v
RaftLogic.handle_message(msg) -> [outgoing...]
        |
        |  server 真正 send
        v
while lastApplied < commitIndex:
        apply
```

Logic 是消息变换器：一进，多出。没有线程，没有队列，没有 socket。`self.send` 只是把消息放进隐藏列表，`handle_message` 返回列表。测试可以断言“对这条 chat，应该出去一条 chat 响应” (D2 2_part019)。

这是从 traffic light 移植的事件循环 (D1 2_part020，D2 1_part026)：

```text
loop:
    event = get()
    logic.handle(event)
    show / send
```

计时器在外层，像红绿灯的 ticker 在逻辑外面 (D4 1_part005)。外层可以：

```text
sleep(heartbeat_interval)
inject("update followers")    # 或只 inject 一个 tick
```

选举超时和心跳的关系只有一条硬的：

```text
follower 必须在选举超时之前听到 leader
因此心跳间隔要小于选举超时
也没有必要小于 ping 时间
```

数值他没给。论文里的 150–300ms 是【Raft 论文补充】。

两种计时器：

| | Follower / Candidate | Leader |
| --- | --- | --- |
| 计时器 | election timeout，随机 | heartbeat interval |
| 到期 | term+1，变 candidate，发 RequestVote | 给每个 follower 发 AppendEntries，entries 可空 |
| 重置 | 收到当前 leader 的 AppendEntries | 不靠这个来保持领导；靠别人没把他选下去 |

Candidate 也要用选举计时器。这一轮没人赢，就再开一轮。

谁拥有“现在该选举了”这个决定，D4 没有结论：

- Server 投递 `UpdateFollowers`，logic 不见钟。
- Server 只投递 tick，logic 计数，心跳和选举都在 logic。学生说这样抽象不漏。
- 单独的 time 对象。学生说它只能看见 tick，否则抽象泄漏。
- Server 当控制器，因为控制台已经这样驱动命令，follower 的沉默检测像控制器的事。

他的担心是算法被拆到两个地方 (D4 1_part031)。【从课程推导】若你只能选一个：把超时事件送进同一个 `handle_message`，让 logic 成为唯一改 `currentTerm` 的地方。钟可以在外面。决定必须在里面。这和他“逻辑无线程”的测试目标一致，也回应了他的担心。他没有宣布这是答案。

全活或全死：无限循环外面包住，意外退出就打印致命错误并 `os._exit`。心跳线程和网络线程也要。一个 util 装饰器或自定义线程启动函数可以集中做。他讨厌 kitchen-sink 模块，但承认自定义启动函数也许是对的。下午他把这个留成可选 (D4 1_part032–1_part035)。

锁：他只明确提过，若多个线程调用 send，send 要锁，而且那会很乱 (D2 1_part025)。他没有说 logic 必须拿一把大锁。他的序列化方式是：logic 不被并发调用。不要把他没说的提升成文档里的锁顺序。

客户端等待是未解的并发问题。Project 1 的线程阻塞在 socket 读上，手里拿着命令，答案要等另一处的 apply。见 [06](06-log-replication.md)。Leader 在 future 还没完成时丢掉领导权，等待者可能永远等不到 (D4 2_part035)。

## 5. Example

三节点上一次心跳和一次选举超时怎么进同一个循环。

```text
时间 →

Server B 的 run loop 阻塞在队列上

网络线程: 收到 A 的 AppendEntries，入队
run loop:  handle → 重置“上次听到 leader 的时刻”
           发出响应
           apply 循环

若队列在整个选举超时内没有来自当前 leader 的消息:
外层计时器入队 ElectionTimeout
run loop:  handle → term+1, candidate, 把 RequestVote 放进出列表
           server 发送
```

不要在 `handle` 里面 `socket.send` 并等待。死掉的 C 会让 B 的选举逻辑停住，A 的下一条消息进不了 `handle`。

Traffic light 的对照 (D1)：

```text
watch_buttons 线程只负责 recv，把按下放进队列
时钟线程或超时只负责把 tick 放进队列
逻辑对象只看 clock、颜色、按钮
断言两种颜色不能相同，包在 handle_event 出口
```

他用 1000 万次随机事件抓一个“27 秒且按钮按下则两边都变绿”的故意 bug。正常运行可能永远不会同时满足这些条件 (D1 2_part037–2_part038)。这就是他后来把断言放进 `handle_message` 前后的原型。

## 6. Invariant

- 改 `currentTerm`、`log`、`commitIndex`、`votedFor`、nextIndex 的代码只在 `handle_message` 里。若做不到，就不要声称逻辑可单测。
- `handle_message` 不阻塞在网络上。
- 计时器到期变成队列里的一条消息，而不是在另一个线程里直接改角色。
- 进程要么整个在跑，要么整个退出。半个 Raft 比崩溃更难调。
- apply 顺序与日志顺序一致，即使 apply 和网络收包在同一个循环里交替。

## 7. Failure Cases

**心跳线程死了，接收线程还在。** Follower 会超时选举，leader 不再复制。症状像分区。`os._exit` 把它变成一次可见的进程死亡。

**接收线程死了，心跳还在。** Leader 继续发，不处理响应，nextIndex 不前进，commit 停。同样难认。

**两个线程同时 handle。** term 检查和追加交错。可能投出两票，或把日志接坏。

**持锁发 RPC。** 对方不响应，选举计时器的回调拿不到锁，集群停在“有锁的沉默”里。这是 lab 里最常见的活性 bug。【MIT 6.5840 实现补充】tester 对这个很敏感。课上他用“不要 send 完立刻 recv”和“connect 可能堵两分钟”靠近了这个问题，没有说“holding lock while sending”。

**Flood 从控制台线程调用 submit。** 和 run loop 并发。他的解析错误掩盖了后面可能的数据竞争 (D5 1_part041)。

## 8. Implementation Notes

网络对象是双重角色：同一个程序既 listen 又 connect。这是他称为 brain-bend 的地方 (D2 1_part008，1_part024)。

```text
listen:  warmup 那套 bind/listen/accept
receive: 不要直接读某一个 socket，因为你不知道五台里谁会说话
         从队列里 pop
send:    第一版可以 connect 再 send
         稳态要复用连接，否则 5–10ms 的更新路径扛不住
死了的 peer: 网络层丢掉这次发送，不要在网络层重试
```

编码放哪，他为了 chat demo 选了在 server 解码，把对象交给 logic (D2 2_part018)。其他切法都还站着：每个组件自己的格式、边缘上的通用消息类、JSON 的 type/destination/text。不要把 demo 的选择写成唯一架构。

Actor 模型他同意可能工作：logic 一个任务，app 一个任务，用 channel 连接。然后他说整个交互很糟，去写代码吧 (D4 2_part010)。

【工程实践补充】生产系统里计时器通常是每个 peer 一个心跳在途窗口，加上选举定时器的随机重置。课上允许每 follower 不同心跳，但没人实现。

## 9. Common Bugs

- 选举超时和心跳用同一个间隔。Follower 在心跳到达的同一拍发起选举。
- 重置计时器的代码在发送线程，处理 AppendEntries 的代码在另一线程，重置丢失。
- 每次 AppendEntries 都新建连接。
- 连接失败时在网络线程里重试十次，把 Raft 的“下一轮心跳再试”做成嵌套重试。
- `unwrap` / 未捕获异常只杀发送线程 (D5 1_part040)。
- 在 `handle_message` 里调用 app，app 再调用 Raft 提交新命令，重入把 log 改乱。
- 用墙钟差值直接当 term。时钟回拨，term 回退。

## 10. Course Insight

他用红绿灯不是为了教你写交通灯。他要一个可以离开硬件仍然成立的规则对象，外加一个把时间变成事件的驱动器。Raft 如果也能这样拆，测试就不必先启动五台机器。

并发模型的核心不是选线程还是 async。是：状态机一次只吃一个事件，网络和时钟只负责把事件放进队列。做不到这一点，Figure 2 的 if 会在你看不见的交错里被执行两次。
