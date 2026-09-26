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

func doMap(taskId int, filename string, nReduce int, mapf func(string, string) []KeyValue) {
	content, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("cannot read %v: %v", filename, err)
	}
	kva := mapf(filename, string(content))

	encs := make([]*json.Encoder, nReduce)
	files := make([]*os.File, nReduce)
	for y := 0; y < nReduce; y++ {
		f, err := os.Create(fmt.Sprintf("mr-%d-%d", taskId, y))
		if err != nil {
			log.Fatalf("cannot create intermediate: %v", err)
		}
		files[y] = f
		encs[y] = json.NewEncoder(f)
	}
	for _, kv := range kva {
		y := ihash(kv.Key) % nReduce
		if err := encs[y].Encode(&kv); err != nil {
			log.Fatalf("cannot encode: %v", err)
		}
	}
	for _, f := range files {
		f.Close()
	}
}

func doReduce(taskId int, nMap int, reducef func(string, []string) string) {
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

	sort.Slice(kva, func(i, j int) bool { return kva[i].Key < kva[j].Key })

	ofile, err := os.Create(fmt.Sprintf("mr-out-%d", taskId))
	if err != nil {
		log.Fatalf("cannot create output: %v", err)
	}
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
		fmt.Fprintf(ofile, "%v %v\n", kva[i].Key, reducef(kva[i].Key, values))
		i = j
	}
	ofile.Close()
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
