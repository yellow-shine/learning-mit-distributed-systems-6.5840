# 14 — 实验、以及他实际怎么调试

## 实验

每个实验按当时的样子写。Visualizer 和 etcd 是他操作的系统。学生的集群大多是口述进度，不是他在录音里单步调试过的代码。

### etcd：一台，杀掉

- Setup: 一台 etcd，客户端 put `name=Guido`、`x=42`，get 能回来。多客户端对应他脑中的上万台机器。
- Action: 杀掉服务器，再 get。
- Observation: 坐一会儿，超时。
- Why: 没有副本。
- Lesson: 这就是注册中心死掉的外部症状 (D1 1_part007)。

### etcd：三台，死一台

- Setup: 脚本拉起三台。
- Action: put/get，杀掉第二台，再操作。
- Observation: 客户端不知道有人死了。
- Why: 还剩多数派，备份能接上。
- Lesson: 1/3 失败是他想要的运行状态 (D1 1_part007)。

### etcd：三台，死两台

- Setup: 同上，再杀一台。
- Action: get。
- Observation: 超时。
- Why: 多数派没了。学生说读应该还可以。Raft 说不行。
- Lesson: 读不是后门。为什么，当天推迟 (D1 1_part008)。D5 才把它接上“必须仍能联系多数派的 leader”。

### etcd：客户端只知道一个地址

- Setup: 三台，杀掉客户端唯一知道的那台，另外两台活着。错误信息里一直是同一个 IP 和端口。
- Action: 再请求。然后把三个端点都写进客户端命令。
- Observation: 只知一个地址时失败。三个地址都给了，才能去试另外两台。粘贴命令时他还有一次没贴对。
- Why: 客户端只跟 leader 说话，而且必须自己知道所有端点。活着的节点不能免费充当注册中心，否则注册中心问题递归。
- Lesson: 看起来像全站故障，可能只是发现信息不全 (D1 1_part008–1_part009)。

### Visualizer：复制一条

- Setup: raft.github.io 上的可视化，他称为 Figure 1 的步骤 2。后来能看见五台。
- Action: 在 leader 上加一条，不要讨论“the two”。再加一条。
- Observation: 过一小会儿，条目出现在其他机器上。点开消息，是 AppendEntries 请求和回复。
- Why: 复制是消息，不是本地赋值。
- Lesson: 步骤 2 里有并发的 RPC (D1 2_part000)。

### Visualizer：停 server 4，再恢复

- Action: 停 server 4，加一条日志，再恢复。
- Observation: 发往死者的消息没有回复，再发，一直发。活着的机器先更新。恢复后下一次发送把条目补上。
- Why: 周期发送既是心跳也是无限重试。没有重试预算。
- Lesson: follower 失败不该让 leader 停止尝试 (D1 2_part001)。

### Visualizer：停 leader

- Action: 看边上的数。收到消息就跳高再下降。停 leader，等最小的那个到 0。
- Observation: 安静，然后那台发起选举并成为 leader。重启五台后，他改口：先变 candidate，发 RequestVote，要有足够日志才能赢。随机是故意的。
- Lesson: 选举不是轮询下一个编号 (D1 2_part001–2_part002)。

### Visualizer：两个 candidate

- Setup: 两个超时接近但不相等。
- Action: 停 leader。
- Observation: server 4 先到，server 5 跟着。两边都变 candidate。消息极多。最后有一个赢。
- Why: 同时超时会发生。
- Lesson: 代码要活过不止一个 candidate。他没展示选不出来的那一轮 (D1 2_part003)。

### Visualizer：不是广播

- Action: 选出 leader，停 server 2，加一条。比较两条同时发出的消息。
- Observation: 一条 prev index 1、prev term 2、entries 空；server 3 到 server 2 是 prev 0、term 0、带着条目。集群当时已经“因为怪原因有点不同步”，他没解释。
- Lesson: 每个 follower 一份定制载荷 (D3 1_part003–1_part004)。

### Visualizer：nextIndex 分叉，然后新 leader 从 7 往回退

- Setup: leader 是 server 3。
- Action: 加请求，看 prev 从 0 到成功后 next=2，再变成 prev=2、next=3。停 S5，再写。再停 S2。然后杀掉 leader。再重启 S5。
- Observation: 活机器 next 到 4、后来有的到 7；死机器停在 3 或 5。三台死后选举无限失败。S5 回来后 S4 当选，把所有 nextIndex 设成 7。对只有两条日志的 S5，false，7→6→5→4。他没等到成功，说理论上会接到。
- Why: nextIndex 是 leader 本地表。新 leader 乐观假设大家一样新。
- Lesson: 初始化加失败回退，是发现过程，不是配置能告诉你的 (D3 1_part036–1_part040)。

### Visualizer：commitIndex 在两台已死时仍是 5

- Action: 停一台，加请求，停第二台，再加。
- Observation: leader 知道的最高位置里有 2 和 5。commitIndex 显示 5。另一张手画的 matchIndex 是 8,5,8,2,7，排序后中间是 7。
- Why: 多数含 leader。死人不回复也不阻止提交。
- Lesson: 中位数是那句“存在一个 N”的一种算法 (D4 2_part001–2_part002)。

### 红绿灯 fuzz

- Setup: 逻辑对象，不变量是两边颜色不能相同。
- Action: 先跑随机事件，期望抓不到 bug。再插入“clock==27 且按钮按下则两边变绿”。跑大约一千万次，打印关掉。
- Observation: 断言马上失败。
- Why: 正常操作可能永远走不到那个状态组合。
- Lesson: 对着不变量 fuzz，而不是只走快乐路径。有人在聊天里提到 TigerBeetle 的加速时钟和断言，他认出是同一类想法 (D1 2_part037–2_part038)。七年来每个实现里都有的丢命令 bug，他留到算法阶段再讲。D5 才说那是丢已提交条目，靠大约一百万次随机才出现。

### Chat 穿过 Figure 1 的盒子

- Setup: 控制台在 Raft server 上，解码在 server，处理在 logic。
- Action: 向某节点发 hello。
- Observation: “1 said hello” 和 “2 got hello”。
- Why: run → handle → send 列表 → 对方 run。逻辑说“发送”，实际只是入列表。
- Lesson: AppendEntries 的骨架。他不关心聊天内容 (D2 2_part017–2_part019)。

### Python append 玩具

- Setup: 日志含 ABC。
- Action: 正确 prev 追加；再追加一次；prev 指向不存在的 2；prev 内容不匹配。
- Observation: 第一次写入，第二次不变，后两次拒绝。
- Lesson: 重试、洞、分歧。Term 是之后的故事，玩具比的是前一项本身 (D2 2_part022–2_part023)。

### 假网络复制

- Setup: 两个 RaftLogic，无 socket。Leader 是 1/2，follower 是 2/2。
- Action: Figure 7 风格，leader 末尾加一条，手工转送多轮消息。
- Observation: 下午的版本他说能工作。早上缺 become-leader 和相等断言，不能跑。一个学生能收到消息但 index 不对，他没有当场修，而是把假网络测试推回去。
- Lesson: 协调可以没有线程。通过不等于真 socket 能通过 (D3 1_part035–1_part036，2_part023–2_part024)。

### 手动 update 控制台

- Setup: 两节点，无计时器，调试控制台。
- Action: 提交类似 `set name Guido`，看日志，输入 update。
- Observation: 他弄崩了控制台，demo 中断。
- Lesson: update 就是心跳的替身。触发应该变成计时器 (D4 1_part005–1_part006)。

### 单节点 KV 返回 pending

- Setup: `raft key value server` 节点 1，成为 leader。客户端连到 1。
- Action: `set x 42`。再拉起更多服务器。
- Observation: 一台时 pending。多台后打印 applying SetX 42。打印不是执行。
- Lesson: 没有多数就没有客户端成功。commit 可以先于“结果回到客户端”这条路存在 (D4 2_part032)。

### 三节点选举

- Setup: 他上午还选不出 leader 的代码，下午看起来通了。
- Action: 一台；加第二台；加第三台；提交；杀 leader；重启一台。
- Observation: 两台选不出。三台有人当选，命令能复制。杀掉之后另外两台回到 candidate。重启后又能选出 leader。
- Lesson: 这是“选举打开之后应该看见的行为”。不是根因分析。他没说上午的 bug 是什么 (D5 1_part028，1_part039)。

### 杀 peer 的 socket 错误

- Setup: 集群已有 leader。
- Action: 杀掉一台 follower，再提交。
- Observation: broken pipe，或 connection refused。他平时不打印，因为会刷屏。学生用 unwrap，线程和应用程序状态一起坏。
- Lesson: 节点死亡不是一个干净的 Raft 事件。发送失败就丢掉这次，让下一轮心跳再试 (D5 1_part040)。

### Flood

- Setup: 控制台命令，约 10 万次 submit。
- Action: 他输入 flood。
- Observation: `invalid literal` / 分割命令的错误。他怀疑 send 不是线程安全。没修完。
- Lesson: 预期就是崩溃或死锁。入口若在状态机外，竞态会来 (D5 1_part041)。

### 随机投递

- Setup: 集群只吃时钟和被洗牌、丢弃、重复的消息。断言已提交条目不变。
- Action: 他下午跑了。也建议大约 10 万次，并故意留一个他以前写过的 bug 看测试能否抓住。
- Observation: 断言响了。不是 Figure 8，term 检查已经在。他把 bug 留在实现里。
- Lesson: 测试能暴露 bug，比这次刚好修掉更值得保留为思考。没有检测“已提交消失”的断言，Figure 8 类错误你根本看不见 (D5 2_part008，2_part034)。

## 调试清单

【课程内容】加上【从课程推导】。推导的条目标了。

他真正坚持的：

1. 日志对象和 logic 必须能在没有 socket 的情况下调用。网络测试是最后一层，而且他讨厌用子进程起真服务器的测试 (D4 1_part000)。
2. 一条消息进，一组消息出。断言出去的是什么，而不是断言线程没死。
3. 先让两份按 Figure 7 摆好的日志收敛，再加节点，再丢包。
4. Figure 6 用来问“谁该当选”，不是用来 fuzz。
5. 控制台要能看日志、提交一条命令、手动触发一次复制。否则五台跑起来你什么都看不见 (D2 1_part006)。
6. 不变量放在 `handle_message` 前后：term 不减，commitIndex 不减，已提交条目还是那一条。
7. 非法状态尽量无法表示。Follower 不要留着 leader 的 nextIndex 表。红绿灯不要用两个颜色变量组合出两边都绿。
8. 线程意外死亡要让整个进程死，并且先打印。否则你会去调活着的那一半。
9. 故意删掉一条规则（例如 current-term 检查），看测试会不会响。响不了，就是你没有检测手段，不是规则多余。
10. 随机：丢、重复、乱序。他拒绝把场景清单发给你。会制造场景是作业的一部分。

建议的日志格式，【从课程推导】。他没有规定这串格式。他的演示依赖 visualizer 里能点开 term、prev index、prev term、entries，以及控制台能看日志。下面的格式是为了在没有 visualizer 时保留同样的信息：

```text
[A] term=4 state=Follower votedFor=B
    recv AppendEntries from C term=5 prev=3/2 entries=1 commit=3
    -> step down to term 5

[C] term=5 state=Leader
    send AppendEntries to A prev=3 prevTerm=2 entries=[4:5:SET x=10] leaderCommit=3
    recv fail from B nextIndex 6 -> 5

[A] commitIndex 2 -> 3 lastApplied 2 -> 3 apply SET x=1
```

每条都带节点、term、角色。RPC 带发送者和接收者。index 变化单独成行。没有这些，五台的打印会变成无法对齐的时间线。他在 flood 和选举失败里的状态，就是“它在做事，但你不知道它以为自己是谁”。

不要：

- 只在出错时打印。选举失败是没有某条成功日志，不是一条异常。
- 把打印留在每条心跳的热路径上又不加开关。他会把 broken pipe 的打印关掉，因为不在的机器会淹没屏幕。关键状态变化要留，重复心跳要能关。
- 用 `print` 在 fuzz 的热循环里。他在一千万次红绿灯测试前把打印拿掉了。

【工程实践补充】确定性仿真比真 socket 更容易重放。他的假网络就是这个方向的最小版。生产系统还会加请求 id 和任期到分布式追踪里。课上没要求。
