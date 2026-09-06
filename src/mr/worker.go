package mr

import "fmt"
import "log"
import "net/rpc"
import "hash/fnv"
import "os"
import "encoding/json"
import "io"
import "sort"
import "time"

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

var coordSockName string // socket for coordinator

// main/mrworker.go calls this function.
func Worker(sockname string, mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	coordSockName = sockname

	for {
		reply := AskReply{}
		if !call("Coordinator.GetTask", &AskArgs{}, &reply) {
			return
		}
		switch reply.Kind {
		case TaskMap:
			doMap(reply.TaskId, reply.File, reply.NReduce, mapf)
			call("Coordinator.ReportTask", &ReportArgs{Kind: TaskMap, TaskId: reply.TaskId}, &ReportReply{})
		case TaskReduce:
			doReduce(reply.TaskId, reply.NMap, reducef)
			call("Coordinator.ReportTask", &ReportArgs{Kind: TaskReduce, TaskId: reply.TaskId}, &ReportReply{})
		case TaskWait:
			time.Sleep(100 * time.Millisecond)
		case TaskExit:
			return
		}
	}
}

// doMap 跑一个 Map 任务：读输入 → 调 mapf → 按 key hash 分到 nReduce 个中间文件。
// 最终文件名 mr-X-Y（X=taskId, Y=reduce 桶），Reduce Y 会读所有 mr-*-Y。
func doMap(taskId int, filename string, nReduce int, mapf func(string, string) []KeyValue) {
	content, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("cannot read %v: %v", filename, err)
	}
	// 用户 Map：一份文件变成一堆 kv，例如 wc 输出 {"the","1"}
	kva := mapf(filename, string(content))

	// 先写临时文件，写完再 rename 成 mr-X-Y，避免 Reduce 读到半成品
	encs := make([]*json.Encoder, nReduce)
	files := make([]*os.File, nReduce)
	tmpNames := make([]string, nReduce)
	for y := 0; y < nReduce; y++ {
		f, err := os.CreateTemp(".", fmt.Sprintf("mr-%d-%d-", taskId, y))
		if err != nil {
			log.Fatalf("cannot create intermediate: %v", err)
		}
		files[y] = f
		tmpNames[y] = f.Name()
		encs[y] = json.NewEncoder(f)
	}
	// 同一 key 永远进同一桶，Reduce 才能把该 key 的所有 value 聚在一起
	for _, kv := range kva {
		y := ihash(kv.Key) % nReduce
		if err := encs[y].Encode(&kv); err != nil {
			log.Fatalf("cannot encode: %v", err)
		}
	}
	for _, f := range files {
		f.Close()
	}
	// 同盘 rename 原子：Reduce 要么看到完整 mr-X-Y，要么看不到
	for y := 0; y < nReduce; y++ {
		if err := os.Rename(tmpNames[y], fmt.Sprintf("mr-%d-%d", taskId, y)); err != nil {
			log.Fatalf("rename: %v", err)
		}
	}
}

// doReduce 跑一个 Reduce 任务：收集所有 Map 产生的 mr-*-taskId 中间文件，
// 按 key 排序后聚合相同 key 的 values，调用 reducef 并将最终结果写入 mr-out-taskId。
func doReduce(taskId int, nMap int, reducef func(string, []string) string) {
	// 1. 读取并反序列化所有 Map 任务写给当前 Reduce 桶 (taskId) 的中间文件
	kva := []KeyValue{}
	for x := 0; x < nMap; x++ {
		f, err := os.Open(fmt.Sprintf("mr-%d-%d", x, taskId))
		if err != nil {
			log.Fatalf("cannot open intermediate: %v", err)
		}
		dec := json.NewDecoder(f)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				if err != io.EOF {
					log.Fatalf("decode: %v", err)
				}
				break
			}
			kva = append(kva, kv)
		}
		f.Close()
	}

	// 2. 按 Key 字典序排序，确保相同的 key 在切片中连续排列，便于分组
	sort.Slice(kva, func(i, j int) bool { return kva[i].Key < kva[j].Key })

	// 3. 先写入临时文件，避免 Reduce 任务执行中崩溃导致留下不完整的输出文件
	tmp, err := os.CreateTemp(".", fmt.Sprintf("mr-out-%d-", taskId))
	if err != nil {
		log.Fatalf("cannot create output: %v", err)
	}

	// 4. 双指针扫描聚合连续相同 key 的所有 value，调用 reducef 并写入结果
	i := 0
	for i < len(kva) {
		j := i + 1
		for j < len(kva) && kva[j].Key == kva[i].Key {
			j++
		}
		values := make([]string, j-i)
		for k := i; k < j; k++ {
			values[k-i] = kva[k].Value
		}
		fmt.Fprintf(tmp, "%v %v\n", kva[i].Key, reducef(kva[i].Key, values))
		i = j
	}
	tmp.Close()

	// 5. 同盘 rename 原子操作：确保外部只看到完整写入的最终结果 mr-out-taskId
	if err := os.Rename(tmp.Name(), fmt.Sprintf("mr-out-%d", taskId)); err != nil {
		log.Fatalf("rename: %v", err)
	}
}

// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	// the "Coordinator.Example" tells the
	// receiving server that we'd like to call
	// the Example() method of struct Coordinator.
	ok := call("Coordinator.Example", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("reply.Y %v\n", reply.Y)
	} else {
		fmt.Printf("call failed!\n")
	}
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	c, err := rpc.DialHTTP("unix", coordSockName)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	if err := c.Call(rpcname, args, reply); err == nil {
		return true
	}
	log.Printf("%d: call failed err %v", os.Getpid(), err)
	return false
}
